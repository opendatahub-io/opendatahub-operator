package modules

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
)

// ModuleReadinessChecker implements dag.ReadinessChecker for
// out-of-tree modules. It looks up the handler by name, fetches
// the module's status, and checks the Ready condition, generation
// staleness, and platform version handshake.
type ModuleReadinessChecker struct {
	registry        *Registry
	client          client.Client
	platformVersion string
	modules         *configv1alpha1.PlatformModules
}

// NewReadinessChecker creates a ReadinessChecker backed by the
// module registry. The platformVersion is the expected version
// from the platform operator release — modules must report this
// version in .status.release.version to be considered ready.
func NewReadinessChecker(reg *Registry, cli client.Client, platformVersion string, opts ...ReadinessCheckerOption) *ModuleReadinessChecker {
	m := &ModuleReadinessChecker{
		registry:        reg,
		client:          cli,
		platformVersion: platformVersion,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// ReadinessCheckerOption configures a ModuleReadinessChecker.
type ReadinessCheckerOption func(*ModuleReadinessChecker)

// WithPlatformModules sets the PlatformModules used to evaluate
// handler-level enablement (DSC managementState). Without this,
// the checker falls back to registry-level enablement only.
func WithPlatformModules(pm *configv1alpha1.PlatformModules) ReadinessCheckerOption {
	return func(m *ModuleReadinessChecker) {
		m.modules = pm
	}
}

// IsReady returns true if the named module is considered ready for DAG
// advancement. When platformVersion is set, the platform release version
// in the module CR status is the sole readiness signal:
//   - empty rv (first deploy, never tracked) → ready, nothing to gate on
//   - rv matches platformVersion → already reconciled at this version
//   - rv mismatches → upgrade in progress, block until re-reconciled
//
// This mirrors the component readiness checker behavior: transient
// runtime failures do not block DAG advancement; only version mismatches
// (upgrade ordering) do.
func (m *ModuleReadinessChecker) IsReady(ctx context.Context, name string) (bool, error) {
	handler := m.registry.Lookup(name)
	if handler == nil {
		return false, fmt.Errorf("module %q: %w", name, dag.ErrUnknownNode)
	}

	if !m.registry.IsEnabled(name) {
		return true, nil
	}

	if m.modules != nil && !handler.IsEnabled(m.modules) {
		return true, nil
	}

	moduleStatus, err := handler.GetModuleStatus(ctx, m.client)
	if err != nil {
		return false, err
	}

	if moduleStatus.ObservedGeneration < moduleStatus.Generation {
		return false, nil
	}

	if m.platformVersion != "" {
		rv := moduleStatus.ReleaseVersion
		return rv == "" || rv == m.platformVersion, nil
	}

	for _, c := range moduleStatus.Conditions {
		if c.Type == status.ConditionTypeReady {
			return c.Status == metav1.ConditionTrue, nil
		}
	}

	return false, nil
}
