package provision

import (
	"context"
	"sync"

	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// UpgradeCheckFunc validates whether a component is ready to be auto-acknowledged
// during an upgrade. Returns nil when the component is healthy.
type UpgradeCheckFunc func(ctx context.Context, reader client.Reader, component, namespace string) error

var (
	upgradeChecksMu sync.RWMutex

	upgradeChecks = map[string]UpgradeCheckFunc{}
)

// RegisterUpgradeCheck registers a component-specific upgrade check.
// If a component has no custom check registered, DefaultUpgradeCheck is used.
func RegisterUpgradeCheck(component string, fn UpgradeCheckFunc) {
	upgradeChecksMu.Lock()
	defer upgradeChecksMu.Unlock()
	upgradeChecks[component] = fn
}

// GetUpgradeCheck returns the upgrade check for a component, falling
// back to DefaultUpgradeCheck when no component-specific check exists.
func GetUpgradeCheck(component string) UpgradeCheckFunc {
	upgradeChecksMu.RLock()
	defer upgradeChecksMu.RUnlock()
	if fn, ok := upgradeChecks[component]; ok {
		return fn
	}
	return DefaultUpgradeCheck
}

// DefaultUpgradeCheck is an explicit no-op for components without a
// component-specific upgrade blocker. Registration still matters so the
// in-tree gate inventory stays aligned with the runtime auto-ack registry.
func DefaultUpgradeCheck(ctx context.Context, _ client.Reader, key string, namespace string) error {
	logf.FromContext(ctx).V(1).Info(
		"skipping default upgrade check",
		"key", key,
		"namespace", namespace,
	)

	return nil
}
