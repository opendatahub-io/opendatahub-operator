package deploy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PrepareManagedDeployment conditionally applies Strategic Merge Patch to revert user-modified fields
// when the deployment is explicitly marked as managed (managed annotation = "true").
//
// Managed Fields:
//   - Container resources (requests/limits): Reverted to manifest values or cleared if manifest is empty
//   - Replicas: Reverted to manifest value or set to 1 (K8s default) if manifest is empty
//
// Behavior:
//   - Only patches when actual drift is detected (deployed != manifest)
//   - Returns early without patching if values already match
//   - Avoids unnecessary patches on every reconcile
func PrepareManagedDeployment(
	ctx context.Context,
	cli client.Client,
	obj *unstructured.Unstructured,
	old *unstructured.Unstructured,
) error {
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
				objResources, objHasResources := objContainerMap["resources"]
				oldResources, oldHasResources := oldContainerMap["resources"]

				if oldHasResources && !objHasResources {
					// Scenario 1: Deployed has resources but manifest doesn't - clear resources
					needsPatch = true
					containerName, _ := objName.(string)
					containerPatches = append(containerPatches, map[string]interface{}{
						"name":      containerName,
						"resources": nil,
					})
				} else if objHasResources && oldHasResources {
					// Scenario 2: Both have resources - check if they differ
					// Use manifest resources in patch to avoid double update (patch + SSA)
					if !reflect.DeepEqual(objResources, oldResources) {
						needsPatch = true
						containerName, _ := objName.(string)
						containerPatches = append(containerPatches, map[string]interface{}{
							"name":      containerName,
							"resources": objResources, // Use manifest resources
						})
					}
				}
				// Scenario 3: Both don't have resources - no patch needed
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
	patchData := map[string]interface{}{
		"spec": map[string]interface{}{},
	}

	spec, ok := patchData["spec"].(map[string]interface{})
	if !ok {
		return errors.New("failed to get spec from patchData")
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
		// Set to 1 (Kubernetes default) to revert user modifications
		spec["replicas"] = int32(1)
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
