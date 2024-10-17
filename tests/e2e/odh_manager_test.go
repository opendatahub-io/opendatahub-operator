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
			"error in validating CRD : featuretrackers.features.opendatahub.io")
	})
}
