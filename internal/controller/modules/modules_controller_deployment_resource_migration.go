package modules

import (
	"context"
	"fmt"

	"github.com/opendatahub-io/odh-platform-utilities/framework/controller/actions/deploy"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

// previousDefaultResourcesTarget identifies the container whose live
// resources are compared against a module's previously shipped defaults.
type previousDefaultResourcesTarget struct {
	containerName string
	defaults      []corev1.ResourceRequirements
}

// newModuleDeploymentCustomizer returns a Deployment apply customizer that
// preserves the framework's user-tuning policy, except when the live primary
// container of a module operator Deployment still carries resources the
// module declared as previously shipped defaults (PreviousDefaultResources).
// An exact match proves the live value was never customized, so the rendered
// replacement is safe to apply during the upgrade.
func newModuleDeploymentCustomizer(reg *Registry) deploy.CustomizerFn {
	return func(
		ctx context.Context,
		cli client.Client,
		action *deploy.Action,
		desired *unstructured.Unstructured,
		current *unstructured.Unstructured,
	) error {
		if current == nil || current.GetAnnotations()[annotations.ManagedByODHOperator] == "true" {
			return deploy.CustomizerFnApplyDeployments(ctx, cli, action, desired, current)
		}

		target, ok := previousDefaultResourcesFor(reg, desired.GetName())
		if !ok {
			return deploy.CustomizerFnApplyDeployments(ctx, cli, action, desired, current)
		}

		migratedCurrent, migrate, err := migrationCurrentDeployment(current, target)
		if err != nil {
			return fmt.Errorf("failed to evaluate previous default resources for Deployment %s/%s: %w",
				current.GetNamespace(), current.GetName(), err)
		}
		if migrate {
			current = migratedCurrent
		}

		return deploy.CustomizerFnApplyDeployments(ctx, cli, action, desired, current)
	}
}

// previousDefaultResourcesFor returns the previous-default target of the
// registered module whose operator Deployment has the given name.
func previousDefaultResourcesFor(reg *Registry, deploymentName string) (previousDefaultResourcesTarget, bool) {
	var (
		target previousDefaultResourcesTarget
		found  bool
	)

	_ = reg.ForAll(func(h ModuleHandler, _ bool) error {
		if found {
			return nil
		}
		p, ok := h.(PreviousDefaultResourcesProvider)
		if !ok {
			return nil
		}
		defaults := p.GetPreviousDefaultResources()
		if len(defaults) == 0 || deploymentNameFor(h, OperatorManifests{}) != deploymentName {
			return nil
		}
		target = previousDefaultResourcesTarget{containerName: containerNameFor(h), defaults: defaults}
		found = true
		return nil
	})

	return target, found
}

// migrationCurrentDeployment returns a copy of current without the target
// container when its resources exactly match one of the previous defaults.
// Dropping the container (rather than only its resources) makes the
// framework merge skip it, so the rendered resources are applied.
func migrationCurrentDeployment(
	current *unstructured.Unstructured,
	target previousDefaultResourcesTarget,
) (*unstructured.Unstructured, bool, error) {
	containers, found, err := unstructured.NestedSlice(current.Object, "spec", "template", "spec", "containers")
	if err != nil || !found {
		return current, false, err
	}

	idx := findNamedContainer(containers, target.containerName)
	if idx < 0 {
		return current, false, nil
	}

	containerMap, ok := containers[idx].(map[string]any)
	if !ok {
		return current, false, nil
	}
	rawResources, ok := containerMap["resources"].(map[string]any)
	if !ok {
		return current, false, nil
	}

	var live corev1.ResourceRequirements
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(rawResources, &live); err != nil {
		return current, false, err
	}
	if !matchesAnyResources(live, target.defaults) {
		return current, false, nil
	}

	filtered := make([]any, 0, len(containers)-1)
	filtered = append(filtered, containers[:idx]...)
	filtered = append(filtered, containers[idx+1:]...)

	migrated := current.DeepCopy()
	if err := unstructured.SetNestedSlice(migrated.Object, filtered, "spec", "template", "spec", "containers"); err != nil {
		return current, false, err
	}

	return migrated, true, nil
}

func matchesAnyResources(live corev1.ResourceRequirements, candidates []corev1.ResourceRequirements) bool {
	for _, candidate := range candidates {
		if equality.Semantic.DeepEqual(live, candidate) {
			return true
		}
	}
	return false
}
