package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"
)

type ModelRegistryTestCtx struct {
	*ComponentTestCtx
}

func modelRegistryTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(t, &componentApi.ModelRegistry{})
	require.NoError(t, err)

	componentCtx := ModelRegistryTestCtx{
		ComponentTestCtx: ct,
	}

	// Define test cases.
	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate component spec", componentCtx.ValidateSpec},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate CRDs reinstated", componentCtx.ValidateCRDReinstated},
		{"Validate component releases", componentCtx.ValidateComponentReleases},
		{"Validate resource deletion recovery", componentCtx.ValidateAllDeletionRecovery},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
		{"Validate CEL allows registriesNamespace changes with omitted managementState", componentCtx.ValidateCELAllowsOmittedManagementState},
		{"Validate CEL allows registriesNamespace changes with Removed managementState", componentCtx.ValidateCELAllowsRemovedManagementState},
		{"Validate CEL blocks registriesNamespace changes with Managed managementState", componentCtx.ValidateCELBlocksManagedManagementState},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateSpec checks the ModelRegistry spec against the DataScienceCluster instance.
func (tc *ModelRegistryTestCtx) ValidateSpec(t *testing.T) {
	t.Helper()

	// Retrieve the DataScienceCluster instance.
	dsc := tc.FetchDataScienceCluster()

	// Validate that the registriesNamespace in ModelRegistry matches the corresponding value in DataScienceCluster spec.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ModelRegistry, types.NamespacedName{Name: componentApi.ModelRegistryInstanceName}),
		WithCondition(jq.Match(`.spec.registriesNamespace == "%s"`, dsc.Spec.Components.ModelRegistry.RegistriesNamespace)),
	)
}

// ValidateCRDReinstated ensures that required CRDs are reinstated if deleted.
func (tc *ModelRegistryTestCtx) ValidateCRDReinstated(t *testing.T) {
	t.Helper()

	crds := []CRD{
		{Name: "modelregistries.modelregistry.opendatahub.io", Version: ""},
	}

	tc.ValidateCRDsReinstated(t, crds)
}

// ValidateCELAllowsOmittedManagementState tests that CEL validation allows registriesNamespace
// changes when managementState is omitted (defaults to "Removed").
// This tests the fix for the "no such key: managementState" error.
func (tc *ModelRegistryTestCtx) ValidateCELAllowsOmittedManagementState(t *testing.T) {
	t.Helper()

	// Remove managementState and set a test registriesNamespace - should succeed
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			withNoManagementState(),
			withRegistriesNamespace("test-omitted-namespace"),
		)),
	)

	// Reset to default for subsequent tests
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			withRemovedManagementState(),
			withRegistriesNamespace("odh-model-registries"),
		)),
	)
}

// ValidateCELAllowsRemovedManagementState tests that CEL validation allows registriesNamespace
// changes when managementState is explicitly set to "Removed".
func (tc *ModelRegistryTestCtx) ValidateCELAllowsRemovedManagementState(t *testing.T) {
	t.Helper()

	// Set managementState to Removed and update registriesNamespace - should succeed
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			withRemovedManagementState(),
			withRegistriesNamespace("test-removed-namespace"),
		)),
	)

	// Reset to default for subsequent tests
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(withRegistriesNamespace("odh-model-registries")),
	)
}

// ValidateCELBlocksManagedManagementState tests that CEL validation blocks registriesNamespace
// changes when managementState is set to "Managed".
func (tc *ModelRegistryTestCtx) ValidateCELBlocksManagedManagementState(t *testing.T) {
	t.Helper()

	// First set managementState to Managed without changing registriesNamespace - should succeed
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(withManagedManagementState()),
	)

	// Now try to change registriesNamespace - this should fail with validation error
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(withRegistriesNamespace("changed-managed-namespace")),
		WithAcceptableErr(k8serr.IsInvalid, "IsInvalid"),
		WithCustomErrorMsg("Expected registriesNamespace update to be blocked when managementState is Managed"),
	)

	// Reset to default for subsequent tests
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			withRemovedManagementState(),
			withRegistriesNamespace("odh-model-registries"),
		)),
	)
}

// Helper functions for ModelRegistry CEL validation tests

// withNoManagementState returns a transform that removes the managementState field entirely.
// This tests the "omitted managementState" scenario that caused the original bug.
func withNoManagementState() testf.TransformFn {
	return testf.Transform(`del(.spec.components.modelregistry.managementState)`)
}

// withRemovedManagementState returns a transform that sets managementState to "Removed".
func withRemovedManagementState() testf.TransformFn {
	return testf.Transform(`.spec.components.modelregistry.managementState = "Removed"`)
}

// withManagedManagementState returns a transform that sets managementState to "Managed".
func withManagedManagementState() testf.TransformFn {
	return testf.Transform(`.spec.components.modelregistry.managementState = "Managed"`)
}

// withRegistriesNamespace returns a transform that sets the registriesNamespace.
func withRegistriesNamespace(namespace string) testf.TransformFn {
	return testf.Transform(`.spec.components.modelregistry.registriesNamespace = "%s"`, namespace)
}
