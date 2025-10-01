package e2e_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

type DataSciencePipelinesTestCtx struct {
	*ComponentTestCtx
}

func dataSciencePipelinesTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(t, &componentApi.DataSciencePipelines{})
	require.NoError(t, err)

	componentCtx := DataSciencePipelinesTestCtx{
		ComponentTestCtx: ct,
	}

	// Define test cases.
	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate component conditions", componentCtx.ValidateConditions},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate component releases", componentCtx.ValidateComponentReleases},
		{"Validate resource deletion recovery", componentCtx.ValidateAllDeletionRecovery},
		{"Validate argoWorkflowsControllers options v1", componentCtx.ValidateArgoWorkflowsControllersOptionsV1},
		{"Validate argoWorkflowsControllers options v2", componentCtx.ValidateArgoWorkflowsControllersOptionsV2},
		{"Validate resource deletion recovery", componentCtx.ValidateAllDeletionRecovery},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateConditions validates that the DataSciencePipelines instance's status conditions are correct.
func (tc *DataSciencePipelinesTestCtx) ValidateConditions(t *testing.T) {
	t.Helper()

	// Ensure the DataSciencePipelines resource has the "ArgoWorkflowAvailable" condition set to "True".
	tc.ValidateComponentCondition(
		gvk.DataSciencePipelines,
		componentApi.DataSciencePipelinesInstanceName,
		status.ConditionArgoWorkflowAvailable,
	)
}

// ValidateArgoWorkflowsControllersOptionsV1 ensures the DataSciencePipelines component is ready if the
// argoWorkflowsControllersSpec options are set to "Removed" when using v1 API (datasciencepipelines field).
func (tc *DataSciencePipelinesTestCtx) ValidateArgoWorkflowsControllersOptionsV1(t *testing.T) {
	t.Helper()

	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceClusterV1, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.datasciencepipelines.argoWorkflowsControllers.managementState = "%s"`, operatorv1.Removed)),
		WithCondition(
			And(
				// Verify v1 condition type exists
				jq.Match(`.status.conditions[] | select(.type == "DataSciencePipelinesReady") | .status == "True"`),
				// Verify v2 condition type does NOT exist
				jq.Match(`[.status.conditions[] | select(.type == "AIPipelinesReady")] | length == 0`),
			),
		),
	)
}

// ValidateArgoWorkflowsControllersOptionsV2 ensures the DataSciencePipelines component is ready if the
// argoWorkflowsControllersSpec options are set to "Removed" when using v2 API (aipipelines field).
func (tc *DataSciencePipelinesTestCtx) ValidateArgoWorkflowsControllersOptionsV2(t *testing.T) {
	t.Helper()

	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.aipipelines.argoWorkflowsControllers.managementState = "%s"`, operatorv1.Removed)),
		WithCondition(
			And(
				// Verify v2 condition type exists
				jq.Match(`.status.conditions[] | select(.type == "AIPipelinesReady") | .status == "True"`),
				// Verify v1 condition type does NOT exist
				jq.Match(`[.status.conditions[] | select(.type == "DataSciencePipelinesReady")] | length == 0`),
			),
		),
	)
}
