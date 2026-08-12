package provision

import (
	"context"
	"errors"
	"fmt"
	"sync"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UpgradeCheckFunc validates whether a component is ready to be auto-acknowledged
// during an upgrade. Returns nil when the component is healthy.
type UpgradeCheckFunc func(ctx context.Context, cli client.Client, component, namespace string) error

var (
	upgradeChecksMu sync.RWMutex

	// TODO(RHOAIENG-82327): replace with real migration checks.
	upgradeChecks = map[string]UpgradeCheckFunc{
		"kserve": func(_ context.Context, _ client.Client, _, _ string) error {
			return errors.New("kserve upgrade check not yet implemented")
		},
	}
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

// DefaultUpgradeCheck verifies that all Deployments carrying the standard
// component label (app.opendatahub.io/<component>: "true") have their
// desired replicas ready. Components with no matching Deployments are
// considered healthy (not yet deployed).
func DefaultUpgradeCheck(ctx context.Context, cli client.Client, component, namespace string) error {
	label := client.MatchingLabels{"app.opendatahub.io/" + component: "true"}

	var deps appsv1.DeploymentList
	if err := cli.List(ctx, &deps, client.InNamespace(namespace), label); err != nil {
		return fmt.Errorf("listing deployments for %s: %w", component, err)
	}

	if len(deps.Items) == 0 {
		return nil
	}

	for i := range deps.Items {
		d := &deps.Items[i]
		desired := int32(1)
		if d.Spec.Replicas != nil {
			desired = *d.Spec.Replicas
		}
		if d.Status.ReadyReplicas < desired {
			return fmt.Errorf("deployment %s/%s not ready: %d/%d replicas",
				d.Namespace, d.Name, d.Status.ReadyReplicas, desired)
		}
	}

	return nil
}
