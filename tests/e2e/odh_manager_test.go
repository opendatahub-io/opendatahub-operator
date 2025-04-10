package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func testODHOperatorValidation(t *testing.T) {
	testCtx, err := NewTestContext()
	require.NoError(t, err)

	t.Run("validate RHOAI Operator pod", testCtx.testODHDeployment)
	t.Run("validate CRDs owned by the operator", testCtx.validateOwnedCRDs)
}

func (tc *testContext) testODHDeployment(t *testing.T) {
	// Verify if the operator deployment is created
	require.NoErrorf(t, tc.waitForOperatorDeployment("rhods-operator", 1),
		"error in validating rhods-operator deployment")
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
		require.NoErrorf(t, tc.validateCRD("dashboards.components.platform.opendatahub.io"),
			"error in validating CRD : dashboards.components.platform.opendatahub.io")
	})

	t.Run("Validate Ray CRD", func(t *testing.T) {
		t.Parallel()
		require.NoErrorf(t, tc.validateCRD("rays.components.platform.opendatahub.io"),
			"error in validating CRD : rays.components.platform.opendatahub.io")
	})

	t.Run("Validate ModelRegistry CRD", func(t *testing.T) {
		t.Parallel()
		require.NoErrorf(t, tc.validateCRD("modelregistries.components.platform.opendatahub.io"),
			"error in validating CRD : modelregistries.components.platform.opendatahub.io")
	})

	t.Run("Validate TrustyAI CRD", func(t *testing.T) {
		t.Parallel()
		require.NoErrorf(t, tc.validateCRD("trustyais.components.platform.opendatahub.io"),
			"error in validating CRD : trustyais.components.platform.opendatahub.io")
	})

	t.Run("Validate Kueue CRD", func(t *testing.T) {
		t.Parallel()
		require.NoErrorf(t, tc.validateCRD("kueues.components.platform.opendatahub.io"),
			"error in validating CRD : kueues.components.platform.opendatahub.io")
	})

	t.Run("Validate TrainingOperator CRD", func(t *testing.T) {
		t.Parallel()
		require.NoErrorf(t, tc.validateCRD("trainingoperators.components.platform.opendatahub.io"),
			"error in validating CRD : trainingoperators.components.platform.opendatahub.io")
	})

	t.Run("Validate FeastOperator CRD", func(t *testing.T) {
		t.Parallel()
		require.NoErrorf(t, tc.validateCRD("feastoperators.components.platform.opendatahub.io"),
			"error in validating CRD : feastoperators.components.platform.opendatahub.io")
	})

	t.Run("Validate DataSciencePipelines CRD", func(t *testing.T) {
		t.Parallel()
		require.NoErrorf(t, tc.validateCRD("datasciencepipelines.components.platform.opendatahub.io"),
			"error in validating CRD : datasciencepipelines.components.platform.opendatahub.io")
	})

	t.Run("Validate Workbenches CRD", func(t *testing.T) {
		t.Parallel()
		require.NoErrorf(t, tc.validateCRD("workbenches.components.platform.opendatahub.io"),
			"error in validating CRD : workbenches.components.platform.opendatahub.io")
	})

	t.Run("Validate Kserve CRD", func(t *testing.T) {
		t.Parallel()
		require.NoErrorf(t, tc.validateCRD("kserves.components.platform.opendatahub.io"),
			"error in validating CRD : kserves.components.platform.opendatahub.io")
	})

	t.Run("Validate ModelMeshServing CRD", func(t *testing.T) {
		t.Parallel()
		require.NoErrorf(t, tc.validateCRD("modelmeshservings.components.platform.opendatahub.io"),
			"error in validating CRD : modelmeshservings.components.platform.opendatahub.io")
	})

	t.Run("Validate ModelController CRD", func(t *testing.T) {
		t.Parallel()
		require.NoErrorf(t, tc.validateCRD("modelcontrollers.components.platform.opendatahub.io"),
			"error in validating CRD : modelcontrollers.components.platform.opendatahub.io")
	})

	t.Run("Validate Monitoring CRD", func(t *testing.T) {
		t.Parallel()
		require.NoErrorf(t, tc.validateCRD("monitorings.services.platform.opendatahub.io"),
			"error in validating CRD : monitorings.services.platform.opendatahub.io")
	})
}
