package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
)

type MLflowOperatorTestCtx struct {
	*ComponentTestCtx
}

func mlflowOperatorTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(t, &componentApi.MLflowOperator{})
	require.NoError(t, err)

	componentCtx := MLflowOperatorTestCtx{
		ComponentTestCtx: ct,
	}

	// Define test cases
	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate resource deletion recovery", componentCtx.ValidateAllDeletionRecovery},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
	}

	// Run the test suite
	RunTestCases(t, testCases)
}
