package modules

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
	platform        *PlatformContext
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

// WithPlatformContext sets the PlatformContext used to evaluate
// handler-level enablement (DSC managementState). Without this,
// the checker falls back to registry-level enablement only.
func WithPlatformContext(p *PlatformContext) ReadinessCheckerOption {
	return func(m *ModuleReadinessChecker) {
		m.platform = p
	}
}

// IsReady returns true if the named module's CR has Ready=True,
// a non-stale observedGeneration, and a release version matching
// the current platform version. Returns an error if the name is
// not found in this registry.
func (m *ModuleReadinessChecker) IsReady(ctx context.Context, name string) (bool, error) {
	handler := m.registry.Lookup(name)
	if handler == nil {
		return false, fmt.Errorf("module %q: %w", name, dag.ErrUnknownNode)
	}

	if !m.registry.IsEnabled(name) {
		return true, nil
	}

	if m.platform != nil && !handler.IsEnabled(m.platform) {
		return true, nil
	}

	moduleStatus, err := handler.GetModuleStatus(ctx, m.client)
	if err != nil {
		return false, err
	}

	if moduleStatus.ObservedGeneration < moduleStatus.Generation {
		return false, nil
	}

	// Version handshake: if the module reports a release version, it must
	// match the platform version. If the module has not populated the
	// field (empty), skip the check and rely on health conditions alone.
	if m.platformVersion != "" && moduleStatus.ReleaseVersion != "" &&
		moduleStatus.ReleaseVersion != m.platformVersion {
		return false, nil
	}

	for _, c := range moduleStatus.Conditions {
		if c.Type == status.ConditionTypeReady {
			return c.Status == metav1.ConditionTrue, nil
		}
	}

	return false, nil
}
