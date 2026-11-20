package modules

import (
	"context"
	"fmt"

	semver "github.com/blang/semver/v4"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
)

// PlatformModuleReadinessChecker gates DAG progression on tracker status.
// The PlatformModule controller writes this release after operator deployment
// and cleanup; it is separate from releases reported by the module CR.
// A matching committed release remains authoritative during runtime failures.
type PlatformModuleReadinessChecker struct {
	registry      *Registry
	client        client.Client
	modules       *configApi.PlatformModules
	targetVersion string
}

func NewReadinessChecker(registry *Registry, cli client.Client, modules *configApi.PlatformModules, targetVersion string) *PlatformModuleReadinessChecker {
	return &PlatformModuleReadinessChecker{
		registry:      registry,
		client:        cli,
		modules:       modules,
		targetVersion: targetVersion,
	}
}

func (c *PlatformModuleReadinessChecker) IsReady(ctx context.Context, name string) (bool, error) {
	if c.registry == nil {
		return false, fmt.Errorf("module %q: %w", name, dag.ErrUnknownNode)
	}
	handler := c.registry.Lookup(name)
	if handler == nil {
		return false, fmt.Errorf("module %q: %w", name, dag.ErrUnknownNode)
	}
	if !c.registry.IsEnabled(name) || !handler.IsEnabled(c.modules) {
		return true, nil
	}

	tracker, err := getPlatformModule(ctx, c.client, name)
	// After a quick re-enable, a terminating tracker can still report a
	// matching release for the operator being removed.
	if err != nil || tracker == nil || !tracker.DeletionTimestamp.IsZero() {
		return false, err
	}

	// New trackers have no release until their first deployment and cleanup
	// complete. A missing OperatorVersion formats as "0.0.0"; check its value.
	version := tracker.Status.Release.Version
	return !version.Equals(semver.Version{}) && version.String() == c.targetVersion, nil
}

// getPlatformModule returns nil until the tracker exists. Its CRD ships with
// this operator, so NoMatch indicates a broken installation rather than a
// module API that is expected to appear later.
func getPlatformModule(ctx context.Context, reader client.Reader, name string) (*configApi.PlatformModule, error) {
	tracker := &configApi.PlatformModule{}
	if err := reader.Get(ctx, client.ObjectKey{Name: name}, tracker); err != nil {
		if k8serr.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("getting PlatformModule %s: %w", name, err)
	}
	return tracker, nil
}
