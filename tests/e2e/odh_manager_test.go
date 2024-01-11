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
	require.NoErrorf(t, tc.waitForControllerDeployment("opendatahub-operator-controller-manager", 1),
		"error in validating odh operator deployment")
}

func (tc *testContext) validateOwnedCRDs(t *testing.T) {
	// Verify if 2 operators CRDs are installed
	require.NoErrorf(t, tc.validateCRD("datascienceclusters.datasciencecluster.opendatahub.io"),
		"error in validating CRD : datascienceclusters.datasciencecluster.opendatahub.io")

	require.NoErrorf(t, tc.validateCRD("dscinitializations.dscinitialization.opendatahub.io"),
		"error in validating CRD : dscinitializations.dscinitialization.opendatahub.io")
}
