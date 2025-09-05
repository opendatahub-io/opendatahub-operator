package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
)

type RayTestCtx struct {
	*ComponentTestCtx
}

func rayTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(t, &componentApi.Ray{})
	require.NoError(t, err)

	componentCtx := RayTestCtx{
		ComponentTestCtx: ct,
	}

	// Define test cases.
	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate component releases", componentCtx.ValidateComponentReleases},
		{"Validate Deployment deletion recovery", componentCtx.ValidateDeploymentDeletionRecovery},
		{"Validate ConfigMap deletion recovery", componentCtx.ValidateConfigMapDeletionRecovery},
		{"Validate Service deletion recovery", componentCtx.ValidateServiceDeletionRecovery},
		{"Validate ServiceAccount deletion recovery", componentCtx.ValidateServiceAccountDeletionRecovery},
		{"Validate RBAC deletion recovery", componentCtx.ValidateRBACDeletionRecovery},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}
