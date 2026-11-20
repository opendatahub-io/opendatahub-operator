package modules

import (
	"context"

	"github.com/opendatahub-io/odh-platform-utilities/framework/controller/actions/deploy"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

const (
	observabilityDeploymentName = "odh-observability"
	managerContainerName        = "manager"
)

var previousObservabilityManagerResources = map[string]any{
	"limits": map[string]any{
		"cpu":    "500m",
		"memory": "512Mi",
	},
	"requests": map[string]any{
		"cpu":    "100m",
		"memory": "128Mi",
	},
}

// customizeModuleDeployments preserves the framework's user-tuning policy,
// except for the one published odh-observability default that was too small.
// An exact match proves the live value was never customized, so the rendered
// replacement is safe to apply during the upgrade.
func customizeModuleDeployments(
	ctx context.Context,
	cli client.Client,
	action *deploy.Action,
	desired *unstructured.Unstructured,
	current *unstructured.Unstructured,
) error {
	if current == nil || desired.GetName() != observabilityDeploymentName ||
		current.GetAnnotations()[annotations.ManagedByODHOperator] == "true" {
		return deploy.CustomizerFnApplyDeployments(ctx, cli, action, desired, current)
	}

	migratedCurrent, migrate, err := migrationCurrentDeployment(current)
	if err != nil {
		return err
	}
	if migrate {
		current = migratedCurrent
	}

	return deploy.CustomizerFnApplyDeployments(ctx, cli, action, desired, current)
}

func migrationCurrentDeployment(current *unstructured.Unstructured) (*unstructured.Unstructured, bool, error) {
	containers, found, err := unstructured.NestedSlice(current.Object, "spec", "template", "spec", "containers")
	if err != nil || !found {
		return current, false, err
	}

	for _, container := range containers {
		containerMap, ok := container.(map[string]any)
		if !ok || containerMap["name"] != managerContainerName {
			continue
		}

		resources, ok := containerMap["resources"].(map[string]any)
		if !ok || !equality.Semantic.DeepEqual(resources, previousObservabilityManagerResources) {
			return current, false, nil
		}

		migrated := current.DeepCopy()
		migratedContainers, _, err := unstructured.NestedSlice(migrated.Object, "spec", "template", "spec", "containers")
		if err != nil {
			return current, false, err
		}

		filtered := make([]any, 0, len(migratedContainers)-1)
		for _, migratedContainer := range migratedContainers {
			containerMap, ok := migratedContainer.(map[string]any)
			if ok && containerMap["name"] == managerContainerName {
				continue
			}
			filtered = append(filtered, migratedContainer)
		}
		if err := unstructured.SetNestedSlice(migrated.Object, filtered, "spec", "template", "spec", "containers"); err != nil {
			return current, false, err
		}

		return migrated, true, nil
	}

	return current, false, nil
}
