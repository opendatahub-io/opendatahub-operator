package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
)

func codeflareTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(&componentApi.CodeFlare{})
	require.NoError(t, err)

	componentCtx := CodeFlareTestCtx{
		ComponentTestCtx: ct,
	}

	t.Run("Validate component enabled", componentCtx.ValidateComponentEnabled)
	t.Run("Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences)
	t.Run("Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources)
	t.Run("Validate component releases", componentCtx.ValidateComponentReleases)
	t.Run("Validate component disabled", componentCtx.ValidateComponentDisabled)
}

type CodeFlareTestCtx struct {
	*ComponentTestCtx
}
