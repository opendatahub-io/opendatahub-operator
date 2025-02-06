package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
)

// TODO: remove unused when test enabled back.
func feastOperatorTestSuite(t *testing.T) { //nolint:unused
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
	t.Run("Validate component releases", componentCtx.ValidateComponentReleases)
}

type FeastOperatorTestCtx struct {
	*ComponentTestCtx
}
