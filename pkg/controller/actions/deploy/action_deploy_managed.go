package deploy

import (
	"context"
	"encoding/json"
	"fmt"

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

	// Check if there's any drift to fix
	needsPatch := false

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
				_, objHasResources := objContainerMap["resources"]
				_, oldHasResources := oldContainerMap["resources"]

				if oldHasResources && !objHasResources {
					// Deployed has resources but manifest doesn't - clear resources
					needsPatch = true
					containerName, ok := objName.(string)
					if !ok {
						continue
					}
					containerPatches = append(containerPatches, map[string]interface{}{
						"name":      containerName,
						"resources": nil,
					})
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

	if oldHasReplicas && !objHasReplicas {
		// Drift detected: old has replicas but manifest doesn't
		needsPatch = true
	}

	// Only apply Strategic Merge Patch if there's actual drift
	if !needsPatch {
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
	if oldHasReplicas && !objHasReplicas {
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
