package e2e_test

import (
	"context"
	"strings"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
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

	const odhNotebookControllerManager = "odh-notebook-controller-manager"

	mlflowEnvMatcher := func(expected string) OmegaMatcher {
		return jq.Match(
			`.spec.template.spec.containers[] | select(.name == "manager") | .env[] | select(.name == "MLFLOW_ENABLED") | .value == "%s"`,
			expected,
		)
	}

	odhControllerDeployment := WithMinimalObject(gvk.Deployment, types.NamespacedName{
		Name:      odhNotebookControllerManager,
		Namespace: tc.AppsNamespace,
	})

	// Ensure MLflowOperator is in Removed state to start the test
	tc.UpdateComponentStateInDataScienceClusterWithKind(operatorv1.Removed, componentApi.MLflowOperatorKind)

	// Ensure the Workbenches component is still ready with MLflowOperator in Removed state
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Workbenches, types.NamespacedName{Name: componentApi.WorkbenchesInstanceName}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`)),
	)

	// Verify the ODH notebook controller deployment exists and is available
	tc.EnsureResourceExists(
		odhControllerDeployment,
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "Available") | .status == "True"`)),
	)

	// Verify MLFLOW_ENABLED env var is "false" when MLflowOperator is Removed in DSC
	tc.EnsureResourceExists(
		odhControllerDeployment,
		WithCondition(mlflowEnvMatcher("false")),
		WithCustomErrorMsg("MLFLOW_ENABLED should be 'false' when MLflowOperator is Removed"),
	)

	// Test the Managed path: enable MLflowOperator and verify MLFLOW_ENABLED env var becomes "true"
	tc.UpdateComponentStateInDataScienceClusterWithKind(operatorv1.Managed, componentApi.MLflowOperatorKind)

	// Verify MLFLOW_ENABLED env var is "true" when MLflowOperator is Managed in DSC
	tc.EnsureResourceExists(
		odhControllerDeployment,
		WithCondition(mlflowEnvMatcher("true")),
		WithCustomErrorMsg("MLFLOW_ENABLED should be 'true' when MLflowOperator is Managed"),
	)

	// Restore MLflowOperator to Removed state
	tc.UpdateComponentStateInDataScienceClusterWithKind(operatorv1.Removed, componentApi.MLflowOperatorKind)

	// Verify MLFLOW_ENABLED env var returns to "false" when MLflowOperator is Removed again in DSC
	tc.EnsureResourceExists(
		odhControllerDeployment,
		WithCondition(mlflowEnvMatcher("false")),
		WithCustomErrorMsg("MLFLOW_ENABLED should return to 'false' when MLflowOperator is Removed again"),
	)
}

// ValidateAllDeletionRecovery overrides the shared ComponentTestCtx method to handle
// workbenches-specific ConfigMap issues. The workbenches component uses Kustomize
// configMapGenerator which appends content-hash suffixes to ConfigMap names. When the
// content changes, a new ConfigMap is created but the old one may not be garbage collected,
// causing the generic deletion recovery test to fail waiting for the orphaned ConfigMap
// to be recreated.
func (tc *WorkbenchesTestCtx) ValidateAllDeletionRecovery(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke, Tier1)

	savedOpts := tc.DefaultResourceOpts
	tc.DefaultResourceOpts = []ResourceOpts{
		WithEventuallyTimeout(tc.TestTimeouts.deletionRecoveryTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	}
	defer func() { tc.DefaultResourceOpts = savedOpts }()

	testCases := []TestCase{
		{"ConfigMap deletion recovery", tc.validateConfigMapDeletionRecovery},
		{"Service deletion recovery", func(t *testing.T) {
			t.Helper()
			tc.ValidateResourceDeletionRecovery(t, gvk.Service, types.NamespacedName{Namespace: tc.AppsNamespace})
		}},
		{"RBAC deletion recovery", tc.ValidateRBACDeletionRecovery},
		{"ServiceAccount deletion recovery", tc.ValidateServiceAccountDeletionRecovery},
		{"Deployment deletion recovery", tc.ValidateDeploymentDeletionRecovery},
	}

	RunTestCases(t, testCases)
}

func (tc *WorkbenchesTestCtx) validateConfigMapDeletionRecovery(t *testing.T) {
	t.Helper()

	nn := types.NamespacedName{Namespace: tc.AppsNamespace}

	existingResources := tc.FetchResources(
		WithMinimalObject(gvk.ConfigMap, nn),
		WithListOptions(&client.ListOptions{
			LabelSelector: k8slabels.Set{
				labels.PlatformPartOf: strings.ToLower(tc.GVK.Kind),
			}.AsSelector(),
			Namespace: nn.Namespace,
		}),
	)

	if len(existingResources) == 0 {
		t.Logf("No ConfigMap resources found for component %s, skipping", tc.GVK.Kind)
		return
	}

	for _, resource := range existingResources {
		name := resource.GetName()

		// Kustomize configMapGenerator appends a content-hash suffix to this ConfigMap.
		// Orphaned copies from previous hashes are not garbage-collected, so the
		// deletion recovery test would fail waiting for the stale copy to be recreated.
		if strings.HasPrefix(name, "odh-notebook-controller-image-parameters") {
			t.Logf("Skipping Kustomize-generated ConfigMap %s (hash suffix causes orphaned copies)", name)
			continue
		}

		t.Run("ConfigMap_"+name, func(t *testing.T) {
			t.Helper()
			tc.EnsureResourceDeletedThenRecreated(
				WithMinimalObject(gvk.ConfigMap, resources.NamespacedNameFromObject(&resource)),
			)
		})
	}
}

func (tc *WorkbenchesTestCtx) ValidateImageStreamsAvailable(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)

	// On vanilla K8s the ImageStream CRD does not exist, so the action
	// returns early without setting any condition — skip in that case.
	exists, err := cluster.HasCRD(context.Background(), tc.Client(), gvk.ImageStream)
	require.NoError(t, err)
	if !exists {
		t.Skip("Skipping ImageStreamsAvailable test: ImageStream CRD not installed (vanilla K8s)")
	}

	// Verify that the Workbenches CR has an ImageStreamsAvailable condition.
	// The condition should exist regardless of whether any ImageStream tags
	// failed to import.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Workbenches, types.NamespacedName{Name: componentApi.WorkbenchesInstanceName}),
		WithCondition(jq.Match(`[.status.conditions[] | select(.type == "ImageStreamsAvailable")] | length > 0`)),
	)
}
