package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	"github.com/opendatahub-io/operator-actions-framework/cluster/gvk"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RevertManagedDeploymentDrift performs a live cluster write via Strategic Merge Patch to clear
// user modifications to managed deployment fields when drift is detected.
func RevertManagedDeploymentDrift(
	ctx context.Context,
	cli client.Client,
	obj *unstructured.Unstructured,
	old *unstructured.Unstructured,
) error {
	if obj.GroupVersionKind() != gvk.Deployment {
		return fmt.Errorf("expected Deployment but got %s", obj.GroupVersionKind())
	}
	if old.GroupVersionKind() != gvk.Deployment {
		return fmt.Errorf("expected Deployment but got %s", old.GroupVersionKind())
	}

	containersPath := []string{"spec", "template", "spec", "containers"}
	replicasPath := []string{"spec", "replicas"}

	objContainers, objFound, err := unstructured.NestedSlice(obj.Object, containersPath...)
	if err != nil {
		return fmt.Errorf("failed to get containers from manifest: %w", err)
	}

	oldContainers, oldFound, err := unstructured.NestedSlice(old.Object, containersPath...)
	if err != nil {
		return fmt.Errorf("failed to get containers from deployed object: %w", err)
	}

	var containerPatches []map[string]any
	if objFound && oldFound {
		for _, objCont := range objContainers {
			objContainerMap, ok := objCont.(map[string]any)
			if !ok {
				continue
			}
			objName, ok := objContainerMap["name"]
			if !ok {
				continue
			}

			for _, oldCont := range oldContainers {
				oldContainerMap, ok := oldCont.(map[string]any)
				if !ok {
					continue
				}
				oldName, ok := oldContainerMap["name"]
				if !ok || oldName != objName {
					continue
				}

				objResources, objHasResources := objContainerMap["resources"]
				if objHasResources {
					objHasResources = !isEmptyResourceMap(objResources)
				}
				_, oldHasResources := oldContainerMap["resources"]

				if oldHasResources && !objHasResources {
					containerPatches = appendClearResourcesPatch(containerPatches, objName)
				} else if objHasResources && oldHasResources {
					oldResources := oldContainerMap["resources"]
					if !equality.Semantic.DeepEqual(objResources, oldResources) {
						if patch := buildResourcesPatch(objName, objResources, oldResources); patch != nil {
							containerPatches = append(containerPatches, patch)
						}
					}
				}
				break
			}
		}
	}

	_, objHasReplicas, err := unstructured.NestedInt64(obj.Object, replicasPath...)
	if err != nil {
		return fmt.Errorf("failed to get replicas from manifest: %w", err)
	}

	_, oldHasReplicas, err := unstructured.NestedInt64(old.Object, replicasPath...)
	if err != nil {
		return fmt.Errorf("failed to get replicas from deployed object: %w", err)
	}

	replicaPatchNeeded := oldHasReplicas && !objHasReplicas

	if !replicaPatchNeeded && len(containerPatches) == 0 {
		return nil
	}

	spec := map[string]any{}
	patchData := map[string]any{
		"spec": spec,
	}

	if len(containerPatches) > 0 {
		spec["template"] = map[string]any{
			"spec": map[string]any{
				"containers": containerPatches,
			},
		}
	}

	if replicaPatchNeeded {
		spec["replicas"] = nil
	}

	patchBytes, err := json.Marshal(patchData)
	if err != nil {
		return fmt.Errorf("failed to marshal patch data for Deployment %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
	}

	if err := cli.Patch(ctx, old, client.RawPatch(types.StrategicMergePatchType, patchBytes)); err != nil {
		return fmt.Errorf("failed to patch managed Deployment %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
	}

	return nil
}

func isEmptyResourceMap(v any) bool {
	m, ok := v.(map[string]any)
	return ok && len(m) == 0
}

func buildResourcesPatch(name, manifestResources, deployedResources any) map[string]any {
	containerName, ok := name.(string)
	if !ok {
		return nil
	}
	manifestMap, ok := manifestResources.(map[string]any)
	if !ok {
		return nil
	}
	deployedMap, ok := deployedResources.(map[string]any)
	if !ok {
		return nil
	}
	resources := make(map[string]any)
	for _, field := range []string{"requests", "limits"} {
		manifest, manifestFound := manifestMap[field].(map[string]any)
		deployed, deployedFound := deployedMap[field].(map[string]any)
		if !manifestFound && !deployedFound {
			continue
		}
		merged := make(map[string]any, len(manifest)+len(deployed))
		maps.Copy(merged, manifest)
		for key := range deployed {
			if _, exists := manifest[key]; !exists {
				merged[key] = nil
			}
		}
		if len(merged) > 0 {
			resources[field] = merged
		}
	}
	return map[string]any{"name": containerName, "resources": resources}
}

func appendClearResourcesPatch(patches []map[string]any, name any) []map[string]any {
	containerName, ok := name.(string)
	if !ok {
		return patches
	}
	return append(patches, map[string]any{
		"name":      containerName,
		"resources": nil,
	})
}
