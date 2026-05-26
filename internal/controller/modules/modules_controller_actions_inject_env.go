package modules

import (
	"context"
	"os"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const applicationsNamespaceEnv = "APPLICATIONS_NAMESPACE"

// injectModuleEnv is a pipeline action that runs after Helm/Kustomize rendering
// and before deploy. It mutates Deployment resources in rr.Resources to inject
// RELATED_IMAGE_* and APPLICATIONS_NAMESPACE environment variables into the
// target container of each module operator Deployment. The target container name
// defaults to "manager" and can be overridden per module via ContainerNamer. If
// the target container is not found, injection is skipped with an error log.
//
// Related images are scoped per module: each module's images are only injected
// into the Deployment matching that module's release name.
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
	return obj.GroupVersionKind() == gvk.Deployment
}

func injectEnvVarsIntoDeployment(log logr.Logger, obj *unstructured.Unstructured, injection *odhtype.ModuleEnvInjection) error {
	containers, found, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
	if err != nil || !found || len(containers) == 0 {
		return err
	}

	var injected int
	deployName := obj.GetName()

	for _, mi := range injection.PerModuleImages {
		if mi.DeploymentName != deployName {
			continue
		}

		targetName := mi.ContainerName
		if targetName == "" {
			targetName = defaultContainerName
		}

		idx := findManagerContainer(containers, targetName)
		if idx < 0 {
			log.Error(nil, "target container not found in Deployment, skipping env injection",
				"deployment", deployName, "container", targetName)
			continue
		}

		container, ok := containers[idx].(map[string]any)
		if !ok {
			continue
		}

		existingEnv, _ := container["env"].([]any)

		for _, relImg := range mi.Images {
			val := os.Getenv(relImg)
			if val == "" {
				log.V(1).Info("RELATED_IMAGE env var not set, skipping",
					"envVar", relImg, "deployment", deployName)
				continue
			}

			if setOrOverrideEnv(&existingEnv, relImg, val) {
				injected++
			}
		}

		if injection.ApplicationsNamespace != "" {
			if setOrOverrideEnv(&existingEnv, applicationsNamespaceEnv, injection.ApplicationsNamespace) {
				injected++
			}
		}

		container["env"] = existingEnv
		containers[idx] = container
	}

	if injected == 0 {
		return nil
	}

	if err := unstructured.SetNestedSlice(obj.Object, containers, "spec", "template", "spec", "containers"); err != nil {
		return err
	}

	log.V(3).Info("injected env vars into module Deployment",
		"name", obj.GetName(),
		"count", injected,
	)

	return nil
}

// setOrOverrideEnv adds a new env var or overrides the value of an existing
// one. Returns true if a change was made (new var added or value changed).
func setOrOverrideEnv(envSlice *[]any, name, value string) bool {
	for i, e := range *envSlice {
		if em, ok := e.(map[string]any); ok {
			if n, ok := em["name"].(string); ok && n == name {
				if em["value"] == value {
					return false
				}
				em["value"] = value
				(*envSlice)[i] = em
				return true
			}
		}
	}

	*envSlice = append(*envSlice, map[string]any{
		"name":  name,
		"value": value,
	})
	return true
}

// findManagerContainer returns the index of the container with the given
// name, or -1 if none is found.
func findManagerContainer(containers []any, targetName string) int {
	for i, c := range containers {
		if cm, ok := c.(map[string]any); ok {
			if name, ok := cm["name"].(string); ok && name == targetName {
				return i
			}
		}
	}

	return -1
}
