package e2e_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
)

// ValidateModelRegistryStatePropagation verifies that ModelRegistry management state
// from the DSC is correctly injected into the Kserve module CR spec.
// This tests the cross-component state injection implemented in handler.go BuildModuleCR.
func (tc *KserveTestCtx) ValidateModelRegistryStatePropagation(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)

	tc.SkipIfXKSCluster(t)

	kserveNN := types.NamespacedName{Name: componentApi.KserveInstanceName}

	// Test 1: ModelRegistry Managed → Kserve CR should have modelRegistry.managementState = Managed
	t.Log("Setting DSC ModelRegistry to Managed")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(func(obj *unstructured.Unstructured) error {
			return unstructured.SetNestedField(obj.Object, "Managed", "spec", "components", "modelregistry", "managementState")
		}),
		WithCondition(
			jq.Match(`.spec.components.modelregistry.managementState == "Managed"`),
		),
	)

	t.Log("Verifying Kserve CR has modelRegistry.managementState = Managed")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Kserve, kserveNN),
		WithCondition(
			jq.Match(`.spec.modelRegistry.managementState == "Managed"`),
		),
		WithCustomErrorMsg("Expected Kserve CR to have spec.modelRegistry.managementState = Managed when DSC ModelRegistry is Managed"),
	)

	// Test 2: ModelRegistry Removed → Kserve CR should have modelRegistry.managementState = Removed
	t.Log("Setting DSC ModelRegistry to Removed")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(func(obj *unstructured.Unstructured) error {
			return unstructured.SetNestedField(obj.Object, "Removed", "spec", "components", "modelregistry", "managementState")
		}),
		WithCondition(
			jq.Match(`.spec.components.modelregistry.managementState == "Removed"`),
		),
	)

	t.Log("Verifying Kserve CR has modelRegistry.managementState = Removed")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Kserve, kserveNN),
		WithCondition(
			jq.Match(`.spec.modelRegistry.managementState == "Removed"`),
		),
		WithCustomErrorMsg("Expected Kserve CR to have spec.modelRegistry.managementState = Removed when DSC ModelRegistry is Removed"),
	)

	// Test 3: ModelRegistry empty → Kserve CR should default to Removed
	t.Log("Unsetting DSC ModelRegistry managementState (empty)")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(func(obj *unstructured.Unstructured) error {
			// Remove the managementState field entirely
			components, found, err := unstructured.NestedMap(obj.Object, "spec", "components", "modelregistry")
			if err != nil || !found {
				return err
			}
			delete(components, "managementState")
			return unstructured.SetNestedMap(obj.Object, components, "spec", "components", "modelregistry")
		}),
		WithCondition(
			jq.Match(`.spec.components.modelregistry.managementState // "" == ""`),
		),
	)

	t.Log("Verifying Kserve CR has modelRegistry.managementState = Removed (default when empty)")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Kserve, kserveNN),
		WithCondition(
			jq.Match(`.spec.modelRegistry.managementState == "Removed"`),
		),
		WithCustomErrorMsg("Expected Kserve CR to have spec.modelRegistry.managementState = Removed (default) when DSC ModelRegistry state is empty"),
	)

	// Restore to Removed for subsequent tests
	t.Log("Restoring DSC ModelRegistry to Removed")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(func(obj *unstructured.Unstructured) error {
			return unstructured.SetNestedField(obj.Object, "Removed", "spec", "components", "modelregistry", "managementState")
		}),
		WithCondition(
			jq.Match(`.spec.components.modelregistry.managementState == "Removed"`),
		),
	)

	t.Log("ModelRegistry state propagation validation passed")
}
