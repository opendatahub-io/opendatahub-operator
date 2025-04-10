package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
)

func feastOperatorTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(&componentApi.FeastOperator{})
	require.NoError(t, err)

	componentCtx := FeastOperatorTestCtx{
		ComponentTestCtx: ct,
	}

	t.Run("Validate component enabled", componentCtx.ValidateComponentEnabled)
	t.Run("Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences)
	t.Run("Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources)
	t.Run("Validate component disabled", componentCtx.ValidateComponentDisabled)
	// TODO: Uncomment the following tests after yaml is in ODH repo
	// t.Run("Validate component releases", componentCtx.ValidateComponentReleases)
}

type FeastOperatorTestCtx struct {
	*ComponentTestCtx
}
