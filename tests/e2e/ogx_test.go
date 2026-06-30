package e2e_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

type OGXTestCtx struct {
	*ComponentTestCtx
}

func ogxTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(t, &componentApi.OGX{})
	require.NoError(t, err)

	componentCtx := OGXTestCtx{
		ComponentTestCtx: ct,
	}

	// Define test cases.
	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate component releases", componentCtx.ValidateComponentReleases},
		{"Validate LlamaStackOperator conflict", componentCtx.ValidateLlamaStackOperatorConflict},
		{"Validate resource deletion recovery", componentCtx.ValidateAllDeletionRecovery},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateLlamaStackOperatorConflict verifies that OGX detects the deprecated
// LlamaStackOperator conflict and recovers when it is resolved. Setting
// LlamaStackOperator to Managed while OGX is enabled should cause the
// precondition to fail; setting it back to Removed should allow recovery
// via the DSC watch event (no requeue needed).
func (tc *OGXTestCtx) ValidateLlamaStackOperatorConflict(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)

	// set LlamaStackOperator to Managed to trigger precondition failure
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.llamastackoperator.managementState = "%s"`, operatorv1.Managed)),
	)

	// validate that OGX conditions reflect the failure
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.OGX, types.NamespacedName{Name: componentApi.OGXInstanceName}),
		WithCondition(
			And(
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionFalse),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionDependenciesAvailable, metav1.ConditionFalse),
			),
		),
		WithCustomErrorMsg("OGX should have Ready and DependenciesAvailable conditions set to False when LlamaStackOperator is Managed"),
	)

	// set LlamaStackOperator back to Removed in DSC to resolve the conflict
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.llamastackoperator.managementState = "%s"`, operatorv1.Removed)),
	)

	// validate OGX recovers after the conflict is resolved
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.OGX, types.NamespacedName{Name: componentApi.OGXInstanceName}),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
		),
		WithCustomErrorMsg("OGX should recover to Ready=True after LlamaStackOperator conflict is resolved"),
	)
}
