package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func testODHOperatorValidation(t *testing.T) {
	testCtx, err := NewTestContext()
	require.NoError(t, err)

	t.Run("validate ODH Operator pod", testCtx.testODHDeployment)
	t.Run("validate CRDs owned by the operator", testCtx.validateOwnedCRDs)
}

func (tc *testContext) testODHDeployment(t *testing.T) {
	// Verify if the operator deployment is created
	require.NoErrorf(t, tc.waitForOperatorDeployment("opendatahub-operator-controller-manager", 1),
		"error in validating odh operator deployment")
}

func (tc *testContext) validateOwnedCRDs(t *testing.T) {
	// Verify if 3 operators CRDs are installed in parallel
	t.Run("Validate DSC CRD", func(t *testing.T) {
		t.Parallel()
		require.NoErrorf(t, tc.validateCRD("datascienceclusters.datasciencecluster.opendatahub.io"),
			"error in validating CRD : datascienceclusters.datasciencecluster.opendatahub.io")
	})
	t.Run("Validate DSCI CRD", func(t *testing.T) {
		t.Parallel()
		require.NoErrorf(t, tc.validateCRD("dscinitializations.dscinitialization.opendatahub.io"),
			"error in validating CRD : dscinitializations.dscinitialization.opendatahub.io")
	})
	t.Run("Validate FeatureTracker CRD", func(t *testing.T) {
		t.Parallel()
		require.NoErrorf(t, tc.validateCRD("featuretrackers.features.opendatahub.io"),
			"error in validating CRD : featuretrackers.features.opendatahub.io")
	})

	// Validate component CRDs
	t.Run("Validate Dashboard CRD", func(t *testing.T) {
		t.Parallel()
		require.NoErrorf(t, tc.validateCRD("dashboards.components.opendatahub.io"),
			"error in validating CRD : dashboards.components.opendatahub.io")
	})

	t.Run("Validate Ray CRD", func(t *testing.T) {
		t.Parallel()
		require.NoErrorf(t, tc.validateCRD("rays.components.opendatahub.io"),
			"error in validating CRD : rays.components.opendatahub.io")
	})

	t.Run("Validate ModelRegistry CRD", func(t *testing.T) {
		t.Parallel()
		require.NoErrorf(t, tc.validateCRD("modelregistries.components.opendatahub.io"),
			"error in validating CRD : modelregistries.components.opendatahub.io")
	})

	t.Run("Validate TrustyAI CRD", func(t *testing.T) {
		t.Parallel()
		require.NoErrorf(t, tc.validateCRD("trustyais.components.opendatahub.io"),
			"error in validating CRD : trustyais.components.opendatahub.io")
	})

	t.Run("Validate Kueue CRD", func(t *testing.T) {
		t.Parallel()
		require.NoErrorf(t, tc.validateCRD("kueues.components.opendatahub.io"),
			"error in validating CRD : kueues.components.opendatahub.io")
	})

	t.Run("Validate TrainingOperator CRD", func(t *testing.T) {
		t.Parallel()
		require.NoErrorf(t, tc.validateCRD("trainingoperators.components.opendatahub.io"),
			"error in validating CRD : trainingoperators.components.opendatahub.io")
	})

	t.Run("Validate DataSciencePipelines CRD", func(t *testing.T) {
		t.Parallel()
		require.NoErrorf(t, tc.validateCRD("datasciencepipelines.components.opendatahub.io"),
			"error in validating CRD : datasciencepipelines.components.opendatahub.io")
	})

	t.Run("Validate Workbenches CRD", func(t *testing.T) {
		t.Parallel()
		require.NoErrorf(t, tc.validateCRD("workbenches.components.opendatahub.io"),
			"error in validating CRD : workbenches.components.opendatahub.io")
	})
}
