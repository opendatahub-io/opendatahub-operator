package deploy

import (
	"context"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

// RevertManagedDeploymentDrift performs a live cluster write via Strategic Merge Patch to clear
// user modifications to managed deployment fields when drift is detected.
//
// Why Strategic Merge Patch is needed:
//   - SSA (Server-Side Apply) can only manage fields that are PRESENT in the manifest
//   - When the manifest intentionally OMITS a field (e.g., empty resources/replicas), SSA cannot
//     remove user-owned values because it doesn't send those fields at all
//   - Strategic Merge Patch can explicitly set fields to nil, clearing user modifications
//   - After this patch, SSA (with ForceOwnership) reclaims ownership of the manifest fields
//
// Managed Fields (only when ABSENT from manifest):
//   - Container resources (requests/limits): Cleared if manifest omits them
//   - Replicas: Set to 1 (K8s default) if manifest omits it
//
// Fields PRESENT in manifest:
//   - Handled natively by SSA with ForceOwnership (no Strategic Merge Patch needed)
//   - SSA automatically reverts user modifications when the field exists in the manifest
func RevertManagedDeploymentDrift(
	ctx context.Context,
	cli client.Client,
	obj *unstructured.Unstructured,
	old *unstructured.Unstructured,
) error {
	// Validate that the objects are Deployments
	if obj.GroupVersionKind() != gvk.Deployment {
		return fmt.Errorf("expected Deployment but got %s", obj.GroupVersionKind())
	}
	if old.GroupVersionKind() != gvk.Deployment {
		return fmt.Errorf("expected Deployment but got %s", old.GroupVersionKind())
	}

	containersPath := []string{"spec", "template", "spec", "containers"}
	replicasPath := []string{"spec", "replicas"}

	// Check container resources drift
	objContainers, objFound, err := unstructured.NestedSlice(obj.Object, containersPath...)
	if err != nil {
		return fmt.Errorf("failed to get containers from manifest: %w", err)
	}

	oldContainers, oldFound, err := unstructured.NestedSlice(old.Object, containersPath...)
	if err != nil {
		return fmt.Errorf("failed to get containers from deployed object: %w", err)
	}

	var containerPatches []map[string]interface{}
	if objFound && oldFound {
		for _, objCont := range objContainers {
			objContainerMap, ok := objCont.(map[string]interface{})
			if !ok {
				continue
			}
			objName, ok := objContainerMap["name"]
			if !ok {
				continue
			}

			// Find matching container in old
			for _, oldCont := range oldContainers {
				oldContainerMap, ok := oldCont.(map[string]interface{})
				if !ok {
					continue
				}
				oldName, ok := oldContainerMap["name"]
				if !ok || oldName != objName {
					continue
				}

				// Check resource drift between manifest and deployed
				objResources, objHasResources := objContainerMap["resources"]
				if objHasResources {
					objHasResources = !isEmptyResourceMap(objResources)
				}
				_, oldHasResources := oldContainerMap["resources"]

				if oldHasResources && !objHasResources {
					// Deployed has resources but manifest doesn't - clear resources
					containerPatches = appendClearResourcesPatch(containerPatches, objName)
				} else if objHasResources && oldHasResources {
					oldResources := oldContainerMap["resources"]
					if !equality.Semantic.DeepEqual(objResources, oldResources) {
						// Build a merged resource patch: manifest values + null for user-added keys.
						// This removes user-added fields in a single SMP write.
						if patch := buildResourcesPatch(objName, objResources, oldResources); patch != nil {
							containerPatches = append(containerPatches, patch)
						}
					}
				}
				break
			}
		}
	}

	// Check replicas drift
	_, objHasReplicas, err := unstructured.NestedInt64(obj.Object, replicasPath...)
	if err != nil {
		return fmt.Errorf("failed to get replicas from manifest: %w", err)
	}

	_, oldHasReplicas, err := unstructured.NestedInt64(old.Object, replicasPath...)
	if err != nil {
		return fmt.Errorf("failed to get replicas from deployed object: %w", err)
	}

	replicaPatchNeeded := oldHasReplicas && !objHasReplicas

	// Only apply Strategic Merge Patch if there's actual drift
	if !replicaPatchNeeded && len(containerPatches) == 0 {
		return nil
	}

	// Build patch data - only include fields that need patching
	spec := map[string]interface{}{}
	patchData := map[string]interface{}{
		"spec": spec,
	}

	// Only include containers if there are container patches
	if len(containerPatches) > 0 {
		spec["template"] = map[string]interface{}{
			"spec": map[string]interface{}{
				"containers": containerPatches,
			},
		}
	}

	// Only include replicas if needed
	if replicaPatchNeeded {
		// Remove replicas field to revert user modifications (Kubernetes will default to 1)
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

func isEmptyResourceMap(v interface{}) bool {
	m, ok := v.(map[string]interface{})
	return ok && len(m) == 0
}

func buildResourcesPatch(name, manifestResources, deployedResources interface{}) map[string]interface{} {
	containerName, ok := name.(string)
	if !ok {
		return nil
	}
	manifestMap, ok := manifestResources.(map[string]interface{})
	if !ok {
		return nil
	}
	deployedMap, ok := deployedResources.(map[string]interface{})
	if !ok {
		return nil
	}
	resources := make(map[string]interface{})
	for _, field := range []string{"requests", "limits"} {
		manifest, manifestFound := manifestMap[field].(map[string]interface{})
		deployed, deployedFound := deployedMap[field].(map[string]interface{})
		if !manifestFound && !deployedFound {
			continue
		}
		merged := make(map[string]interface{}, len(manifest)+len(deployed))
		for key, val := range manifest {
			merged[key] = val
		}
		for key := range deployed {
			if _, exists := manifest[key]; !exists {
				merged[key] = nil
			}
		}
		if len(merged) > 0 {
			resources[field] = merged
		}
	}
	return map[string]interface{}{"name": containerName, "resources": resources}
}

func appendClearResourcesPatch(patches []map[string]interface{}, name interface{}) []map[string]interface{} {
	containerName, ok := name.(string)
	if !ok {
		return patches
	}
	return append(patches, map[string]interface{}{
		"name":      containerName,
		"resources": nil,
	})
}
