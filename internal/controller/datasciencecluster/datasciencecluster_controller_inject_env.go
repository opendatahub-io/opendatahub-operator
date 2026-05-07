package datasciencecluster

import (
	"context"
	"os"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// injectModuleEnv is a pipeline action that runs after Helm/Kustomize rendering
// and before deploy. It mutates Deployment resources in rr.Resources to inject
// RELATED_IMAGE_* and APPLICATIONS_NAMESPACE environment variables into the
// "manager" container (or the first container if none is named "manager") of
// each module operator Deployment.
//
// The injection data is read from rr.ModuleEnvInjection (set by provisionModules).
// If nil, this action is a no-op.
func injectModuleEnv(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	if rr.ModuleEnvInjection == nil {
		return nil
	}

	log := logf.FromContext(ctx)

	for i := range rr.Resources {
		if !isDeployment(&rr.Resources[i]) {
			continue
		}

		if err := injectEnvVarsIntoDeployment(log, &rr.Resources[i], rr.ModuleEnvInjection); err != nil {
			log.Error(err, "failed to inject env vars into Deployment",
				"name", rr.Resources[i].GetName(),
				"namespace", rr.Resources[i].GetNamespace(),
			)

			return err
		}
	}

	return nil
}

func isDeployment(obj *unstructured.Unstructured) bool {
	gvk := obj.GroupVersionKind()
	return gvk.Group == "apps" && gvk.Kind == "Deployment"
}

func injectEnvVarsIntoDeployment(log logr.Logger, obj *unstructured.Unstructured, injection *odhtype.ModuleEnvInjection) error {
	containers, found, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
	if err != nil || !found || len(containers) == 0 {
		return err
	}

	idx := findManagerContainer(containers)

	container, ok := containers[idx].(map[string]any)
	if !ok {
		return nil
	}

	existingEnv, _ := container["env"].([]any)
	existingNames := make(map[string]bool, len(existingEnv))

	for _, e := range existingEnv {
		if em, ok := e.(map[string]any); ok {
			if name, ok := em["name"].(string); ok {
				existingNames[name] = true
			}
		}
	}

	var injected int

	for _, relImg := range injection.RelatedImages {
		if existingNames[relImg] {
			continue
		}

		val := os.Getenv(relImg)
		if val == "" {
			continue
		}

		existingEnv = append(existingEnv, map[string]any{
			"name":  relImg,
			"value": val,
		})
		injected++
	}

	if injection.ApplicationsNamespace != "" && !existingNames["APPLICATIONS_NAMESPACE"] {
		existingEnv = append(existingEnv, map[string]any{
			"name":  "APPLICATIONS_NAMESPACE",
			"value": injection.ApplicationsNamespace,
		})
		injected++
	}

	if injected == 0 {
		return nil
	}

	container["env"] = existingEnv
	containers[idx] = container

	if err := unstructured.SetNestedSlice(obj.Object, containers, "spec", "template", "spec", "containers"); err != nil {
		return err
	}

	log.V(3).Info("injected env vars into module Deployment",
		"name", obj.GetName(),
		"container", container["name"],
		"count", injected,
	)

	return nil
}

// findManagerContainer returns the index of the container named "manager"
// in the list, or 0 if none is found. The "manager" name is the
// controller-runtime convention for the primary operator container.
func findManagerContainer(containers []any) int {
	for i, c := range containers {
		if cm, ok := c.(map[string]any); ok {
			if name, ok := cm["name"].(string); ok && name == "manager" {
				return i
			}
		}
	}

	return 0
}
