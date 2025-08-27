package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
)

type TrainingOperatorTestCtx struct {
	*ComponentTestCtx
}

func trainingOperatorTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(t, &componentApi.TrainingOperator{})
	require.NoError(t, err)

	componentCtx := TrainingOperatorTestCtx{
		ComponentTestCtx: ct,
	}

	// Define test cases.
	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate component releases", componentCtx.ValidateComponentReleases},
		{"Validate deployment deletion recovery", componentCtx.ValidateDeploymentDeletionRecovery},
		{"Validate configmap deletion recovery", componentCtx.ValidateConfigMapDeletionRecovery},
		{"Validate service deletion recovery", componentCtx.ValidateServiceDeletionRecovery},
		// {"Validate rbac deletion recovery", componentCtx.ValidateRBACDeletionRecovery},
		{"Validate serviceaccount deletion recovery", componentCtx.ValidateServiceAccountDeletionRecovery},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}
