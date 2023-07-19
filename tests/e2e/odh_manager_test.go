package e2e

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func testODHOperatorValidation(t *testing.T) {
	testCtx, err := NewTestContext()
	require.NoError(t, err)
	t.Run("Validate ODH Operator pod", testCtx.testODHDeployment)
	t.Run("Validate CRDs owned by the operator", testCtx.validateOwnedCRDs)
}

func (tc *testContext) testODHDeployment(t *testing.T) {
	// Verify if the operator pod is running
	require.NoErrorf(t, tc.waitForControllerDeployment("opendatahub-operator-controller-manager", 1),
		"error in validating odh operator deployment")
}

func (tc *testContext) validateOwnedCRDs(t *testing.T) {
	// Verify if all the required CRDs are installed
	require.NoErrorf(t, tc.validateCRD("datascienceclusters.datasciencecluster.opendatahub.io"),
		"error in validating CRD : datascienceclusters.datasciencecluster.opendatahub.io")

	require.NoErrorf(t, tc.validateCRD("dscinitializations.dscinitialization.opendatahub.io"),
		"error in validating CRD : dscinitializations.dscinitialization.opendatahub.io")

	require.NoErrorf(t, tc.validateCRD("odhquickstarts.console.openshift.io"),
		"error in validating CRD : odhquickstarts.console.openshift.io")

	require.NoErrorf(t, tc.validateCRD("odhapplications.dashboard.opendatahub.io"),
		"error in validating CRD : odhapplications.dashboard.opendatahub.io")

	require.NoErrorf(t, tc.validateCRD("odhdashboardconfigs.opendatahub.io"),
		"error in validating CRD : odhdashboardconfigs.opendatahub.io")

	require.NoErrorf(t, tc.validateCRD("odhdocuments.dashboard.opendatahub.io"),
		"error in validating CRD : odhdocuments.dashboard.opendatahub.io")
}
