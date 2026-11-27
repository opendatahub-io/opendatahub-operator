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
// The module CR release is checked as well: it is written by the module
// operator itself, so a dead or quota-blocked operator keeps its module CR
// at the previous release and the batch stays gated until the module is
// actually Ready at the target version.
type PlatformModuleReadinessChecker struct {
	registry      *Registry
	client        client.Client
	platform      *configApi.Platform
	targetVersion string
}

func NewReadinessChecker(registry *Registry, cli client.Client, platform *configApi.Platform, targetVersion string) *PlatformModuleReadinessChecker {
	return &PlatformModuleReadinessChecker{
		registry:      registry,
		client:        cli,
		platform:      platform,
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
	if !c.registry.IsEnabled(name) || !handler.IsEnabled(&c.platform.Spec.Modules) {
		return true, nil
	}

	tracker, err := getPlatformModule(ctx, c.client, name)
	// After a quick re-enable, a terminating tracker can still report a
	// matching release for the operator being removed.
	if err != nil || tracker == nil || !tracker.DeletionTimestamp.IsZero() {
		return false, err
	}

	// Foreign trackers must not open this Platform's runlevels.
	if !PlatformOwnsResource(c.platform, tracker) {
		return false, nil
	}

	// New trackers have no release until their first deployment and cleanup
	// complete. A missing OperatorVersion formats as "0.0.0"; check its value.
	version := tracker.Status.Release.Version
	if version.Equals(semver.Version{}) || version.String() != c.targetVersion {
		return false, nil
	}

	// Operator resources are committed at the target release. The module CR
	// must also report convergence at the same release: it is written by the
	// module operator, so runtime failures (dead operator pods, quota-blocked
	// scheduling) keep the previous release and the batch stays gated. An
	// empty value means the module operator never reported one (first deploy
	// or pre-feature CR) and does not block.
	moduleStatus, err := handler.GetModuleStatus(ctx, c.client)
	if err != nil {
		return false, fmt.Errorf("module %q status: %w", name, err)
	}
	if rv := moduleStatus.ReleaseVersion; rv != "" && rv != c.targetVersion {
		return false, nil
	}
	return true, nil
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
