package e2e

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func testKfDefControllerValidation(t *testing.T) {
	testCtx, err := NewTestContext()
	require.NoError(t, err)
	t.Run("Validate KfDef controller", testCtx.testODHDeployment)
	t.Run("Validate CRDs owned by the operator", testCtx.validateOwnedCRDs)
}

func (tc *testContext) testODHDeployment(t *testing.T) {
	// Verify if the operator pod is running
	require.NoErrorf(t, tc.waitForControllerDeployment("opendatahub-operator-controller-manager", 1),
		"error in validating odh operator deployment")
}

func (tc *testContext) validateOwnedCRDs(t *testing.T) {
	// Verify if all the required CRDs are installed
	require.NoErrorf(t, tc.validateCRD("kfdefs.kfdef.apps.kubeflow.org"),
		"error in validating CRD : kfdefs.kfdef.apps.kubeflow.org")

	require.NoErrorf(t, tc.validateCRD("odhquickstarts.console.openshift.io"),
		"error in validating CRD : odhquickstarts.console.openshift.io")

	require.NoErrorf(t, tc.validateCRD("odhapplications.dashboard.opendatahub.io"),
		"error in validating CRD : odhapplications.dashboard.opendatahub.io")

	require.NoErrorf(t, tc.validateCRD("odhdashboardconfigs.opendatahub.io"),
		"error in validating CRD : odhdashboardconfigs.opendatahub.io")

	require.NoErrorf(t, tc.validateCRD("odhdocuments.dashboard.opendatahub.io"),
		"error in validating CRD : odhdocuments.dashboard.opendatahub.io")
}
