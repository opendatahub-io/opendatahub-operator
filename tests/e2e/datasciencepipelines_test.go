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
		{"Validate argoWorkflowsControllers options", componentCtx.ValidateArgoWorkflowsControllersOptions},
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

// ValidateArgoWorkflowsControllersOptions ensures the DataSciencePipelines component is ready if the
// argoWorkflowsControllersSpec options are set to "Removed".
func (tc *DataSciencePipelinesTestCtx) ValidateArgoWorkflowsControllersOptions(t *testing.T) {
	t.Helper()

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.datasciencepipelines.argoWorkflowsControllers.managementState = "%s"`, operatorv1.Removed)),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "DataSciencePipelinesReady") | .status == "True"`),
		),
	)
}
