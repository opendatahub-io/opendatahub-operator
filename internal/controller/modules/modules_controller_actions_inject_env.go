package modules

import (
	"context"
	"fmt"
	"os"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const (
	applicationsNamespaceEnv = "APPLICATIONS_NAMESPACE"
	monitoringNamespaceEnv   = "MONITORING_NAMESPACE"
)

// injectModuleEnv is a pipeline action that runs after Helm/Kustomize rendering
// and before deploy. It mutates Deployment resources in rr.Resources to inject
// RELATED_IMAGE_*, APPLICATIONS_NAMESPACE and MONITORING_NAMESPACE environment variables into the
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
	if err != nil {
		return err
	}
	if !found || len(containers) == 0 {
		return fmt.Errorf("deployment %s has no containers", obj.GetName())
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

		idx := findNamedContainer(containers, targetName)
		if idx < 0 {
			log.Error(nil, "target container not found in Deployment, skipping env injection",
				"deployment", deployName, "container", targetName)
			continue
		}

		container, ok := containers[idx].(map[string]any)
		if !ok {
			continue
		}

		if mi.ControllerImage != "" {
			if img := os.Getenv(mi.ControllerImage); img != "" {
				container["image"] = img
				injected++
				log.V(3).Info("overriding controller image",
					"deployment", deployName, "container", targetName,
					"envVar", mi.ControllerImage)

				initInjected, err := injectInitContainerImage(log, obj, mi.InitContainerName, img, deployName, mi.ControllerImage)
				if err != nil {
					return err
				}
				injected += initInjected
			} else {
				log.V(1).Info("controller image env var not set, keeping chart default",
					"envVar", mi.ControllerImage, "deployment", deployName)
			}
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

		if injection.MonitoringNamespace != "" {
			if setOrOverrideEnv(&existingEnv, monitoringNamespaceEnv, injection.MonitoringNamespace) {
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

// findNamedContainer returns the index of the container with the given
// name, or -1 if none is found.
func findNamedContainer(containers []any, targetName string) int {
	for i, c := range containers {
		if cm, ok := c.(map[string]any); ok {
			if name, ok := cm["name"].(string); ok && name == targetName {
				return i
			}
		}
	}

	return -1
}

// injectInitContainerImage overrides the image field on a named init container
// with the resolved controller image. Returns 1 if patched, 0 otherwise.
func injectInitContainerImage(
	log logr.Logger,
	obj *unstructured.Unstructured,
	initContainerName string,
	img string,
	deployName string,
	envVar string,
) (int, error) {
	if initContainerName == "" {
		return 0, nil
	}

	initContainers, found, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "initContainers")
	if err != nil {
		return 0, err
	}
	if !found || len(initContainers) == 0 {
		log.V(1).Info("no initContainers in Deployment, skipping init container image injection",
			"deployment", deployName)
		return 0, nil
	}

	idx := findNamedContainer(initContainers, initContainerName)
	if idx < 0 {
		return 0, fmt.Errorf("init container %q not found in Deployment %s", initContainerName, deployName)
	}

	ic, ok := initContainers[idx].(map[string]any)
	if !ok {
		return 0, nil
	}

	ic["image"] = img
	initContainers[idx] = ic

	if err := unstructured.SetNestedSlice(obj.Object, initContainers, "spec", "template", "spec", "initContainers"); err != nil {
		return 0, err
	}

	log.V(3).Info("overriding init container image",
		"deployment", deployName, "initContainer", initContainerName,
		"envVar", envVar)

	return 1, nil
}
