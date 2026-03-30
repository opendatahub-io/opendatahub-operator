package e2e_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

type WorkbenchesTestCtx struct {
	*ComponentTestCtx
}

func workbenchesTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(t, &componentApi.Workbenches{})
	require.NoError(t, err)

	componentCtx := WorkbenchesTestCtx{
		ComponentTestCtx: ct,
	}

	// Define test cases.
	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate workbenches namespace configuration", componentCtx.ValidateWorkbenchesNamespaceConfiguration},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate component releases", componentCtx.ValidateComponentReleases},
		{"Validate ImageStreams available", componentCtx.ValidateImageStreamsAvailable},
		{"Validate MLflow integration", componentCtx.ValidateMLflowIntegration},
		{"Validate resource deletion recovery", componentCtx.ValidateAllDeletionRecovery},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

func (tc *WorkbenchesTestCtx) ValidateWorkbenchesNamespaceConfiguration(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)

	// ensure the workbenches namespace exists and has the expected label
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: tc.WorkbenchesNamespace}),
		WithCondition(jq.Match(`.metadata.labels["%s"] == "true"`, labels.ODH.OwnedNamespace)),
	)

	// ensure the DataScienceCluster has the expected workbench namespace
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(`.spec.components.workbenches.workbenchNamespace == "%s"`, tc.WorkbenchesNamespace)),
	)

	// ensure the Workbenches CR instance has the expected workbench namespace in both spec and status
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Workbenches, types.NamespacedName{Name: componentApi.WorkbenchesInstanceName}),
		WithCondition(
			And(
				jq.Match(`.spec.workbenchNamespace == "%s"`, tc.WorkbenchesNamespace),
				jq.Match(`.status.workbenchNamespace == "%s"`, tc.WorkbenchesNamespace),
			),
		),
	)
}

func (tc *WorkbenchesTestCtx) ValidateMLflowIntegration(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier2)

	g := tc.NewWithT(t)

	const notebookControllerParamsConfigMap = "odh-notebook-controller-image-parameters"

	// Get the current DSC
	dsc := tc.FetchDataScienceCluster()

	// Verify MLflowOperator is in Removed state (default for e2e tests)
	g.Expect(dsc.Spec.Components.MLflowOperator.ManagementState).To(Equal(operatorv1.Removed),
		"MLflowOperator should be in Removed state by default")

	// Ensure the Workbenches component is still ready with MLflowOperator in Removed state
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Workbenches, types.NamespacedName{Name: componentApi.WorkbenchesInstanceName}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`)),
	)

	// Verify the notebook controller deployment exists and is available
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      "notebook-controller-deployment",
			Namespace: tc.AppsNamespace,
		}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "Available") | .status == "True"`)),
	)

	// Verify mlflow-enabled is "false" when MLflowOperator is Removed
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ConfigMap, types.NamespacedName{
			Name:      notebookControllerParamsConfigMap,
			Namespace: tc.AppsNamespace,
		}),
		WithCondition(jq.Match(`.data["mlflow-enabled"] == "false"`)),
		WithCustomErrorMsg("mlflow-enabled should be 'false' when MLflowOperator is Removed"),
	)

	t.Log("Verified mlflow-enabled is 'false' when MLflowOperator is Removed")

	// Test the Managed path: enable MLflowOperator and verify mlflow-enabled becomes "true"
	tc.UpdateComponentStateInDataScienceClusterWithKind(operatorv1.Managed, componentApi.MLflowOperatorKind)

	// Verify mlflow-enabled is "true" when MLflowOperator is Managed
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ConfigMap, types.NamespacedName{
			Name:      notebookControllerParamsConfigMap,
			Namespace: tc.AppsNamespace,
		}),
		WithCondition(jq.Match(`.data["mlflow-enabled"] == "true"`)),
		WithCustomErrorMsg("mlflow-enabled should be 'true' when MLflowOperator is Managed"),
	)

	t.Log("Verified mlflow-enabled is 'true' when MLflowOperator is Managed")

	// Restore MLflowOperator to Removed state
	tc.UpdateComponentStateInDataScienceClusterWithKind(operatorv1.Removed, componentApi.MLflowOperatorKind)

	// Verify mlflow-enabled returns to "false" when MLflowOperator is Removed again
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ConfigMap, types.NamespacedName{
			Name:      notebookControllerParamsConfigMap,
			Namespace: tc.AppsNamespace,
		}),
		WithCondition(jq.Match(`.data["mlflow-enabled"] == "false"`)),
		WithCustomErrorMsg("mlflow-enabled should return to 'false' when MLflowOperator is Removed again"),
	)

	t.Log("Workbenches component successfully integrates with MLflowOperator state changes")
}

func (tc *WorkbenchesTestCtx) ValidateImageStreamsAvailable(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)

	// Verify that the Workbenches CR has an ImageStreamsAvailable condition.
	// The condition should exist regardless of whether any ImageStream tags
	// failed to import. Fix for RHOAIENG-13921.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Workbenches, types.NamespacedName{Name: componentApi.WorkbenchesInstanceName}),
		WithCondition(jq.Match(`[.status.conditions[] | select(.type == "ImageStreamsAvailable")] | length > 0`)),
	)
}
