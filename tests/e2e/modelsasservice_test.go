package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
)

type ModelsAsServiceTestCtx struct {
	*ComponentTestCtx
}

const (
	// Subcomponent field name in JSON (matches the field name in KserveCommonSpec).
	// This must match the JSON tag in KserveCommonSpec.ModelsAsService.
	modelsAsServiceFieldName = "modelsAsService"
)

func modelsAsServiceTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewSubComponentTestCtx(t, &componentApi.ModelsAsService{}, componentApi.KserveKind, modelsAsServiceFieldName)
	require.NoError(t, err)

	componentCtx := ModelsAsServiceTestCtx{
		ComponentTestCtx: ct,
	}

	testCases := []TestCase{
		{"Validate subcomponent enabled", componentCtx.ValidateSubComponentEnabled},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate subcomponent releases", componentCtx.ValidateSubComponentReleases},
		{"Validate resource deletion recovery", componentCtx.ValidateAllDeletionRecovery},
		{"Validate subcomponent disabled", componentCtx.ValidateSubComponentDisabled},
	}

	RunTestCases(t, testCases)
}
