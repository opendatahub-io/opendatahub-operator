package e2e_test

import (
	"context"
	"strings"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	workbenchesModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/workbenches"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
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

	ct, err := NewModuleTestCtx(t, gvk.Workbenches, componentApi.WorkbenchesInstanceName)
	require.NoError(t, err)

	componentCtx := WorkbenchesTestCtx{
		ComponentTestCtx: ct,
	}

	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate module operator deployment", componentCtx.ValidateModuleOperatorDeployment},
		{"Validate workbenches namespace configuration", componentCtx.ValidateWorkbenchesNamespaceConfiguration},
		{"Validate module releases", componentCtx.ValidateModuleReleases},
		{"Validate ImageStreams available", componentCtx.ValidateImageStreamsAvailable},
		{"Validate MLflow integration", componentCtx.ValidateMLflowIntegration},
		{"Validate resource deletion recovery", componentCtx.ValidateAllDeletionRecovery},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
	}

	RunTestCases(t, testCases)
}

// ValidateComponentEnabled ensures the module CR is ready and DSC module conditions are satisfied.
func (tc *WorkbenchesTestCtx) ValidateComponentEnabled(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke, Tier1)

	tc.ComponentTestCtx.ValidateComponentEnabled(t)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Namespace: tc.AppsNamespace,
			Name:      workbenchesModule.ControllerDeploymentName,
		}),
		WithCondition(jq.Match(`.status.readyReplicas >= 1`)),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeModulesReady, metav1.ConditionTrue)),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, componentApi.WorkbenchesKind, metav1.ConditionTrue)),
		WithCustomErrorMsg("DataScienceCluster should have %sReady condition set to True", componentApi.WorkbenchesKind),
	)
}

// ValidateModuleOperatorDeployment verifies the out-of-tree module operator Deployment is ready.
func (tc *WorkbenchesTestCtx) ValidateModuleOperatorDeployment(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke, Tier1)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Namespace: tc.AppsNamespace,
			Name:      workbenchesModule.ControllerDeploymentName,
		}),
		WithCondition(jq.Match(`.status.readyReplicas >= 1`)),
	)
}

// ValidateModuleReleases ensures the Workbenches module CR exposes release metadata.
// Typed status (releases, workbenchNamespace) lives on the module CR owned by
// workbenches-operator; the platform does not project it into DSC status.
func (tc *WorkbenchesTestCtx) ValidateModuleReleases(t *testing.T) {
	t.Helper()

	tc.SkipIfXKSCluster(t)

	skipUnless(t, Smoke)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Workbenches, types.NamespacedName{Name: componentApi.WorkbenchesInstanceName}),
		WithCondition(
			And(
				jq.Match(`[.status.releases[]? | select(.name != "" and .version != "" and .repoUrl != "")] | length > 0`),
			),
		),
		WithCustomErrorMsg("Workbenches module CR should expose non-empty status.releases entries"),
	)
}

func (tc *WorkbenchesTestCtx) ValidateWorkbenchesNamespaceConfiguration(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: tc.WorkbenchesNamespace}),
		WithCondition(jq.Match(`.metadata.labels["%s"] == "true"`, labels.ODH.OwnedNamespace)),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(`.spec.components.workbenches.workbenchNamespace == "%s"`, tc.WorkbenchesNamespace)),
	)

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

	// Operands are reconciled into spec.workbenchNamespace, not the applications namespace
	// where workbenches-operator runs (see ValidateWorkbenchesNamespaceConfiguration).
	odhControllerDeployment := WithMinimalObject(gvk.Deployment, types.NamespacedName{
		Name:      odhNotebookControllerManager,
		Namespace: tc.WorkbenchesNamespace,
	})

	tc.UpdateComponentStateInDataScienceClusterWithKind(operatorv1.Removed, componentApi.MLflowOperatorKind)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Workbenches, types.NamespacedName{Name: componentApi.WorkbenchesInstanceName}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`)),
	)

	tc.EnsureResourceExists(
		odhControllerDeployment,
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "Available") | .status == "True"`)),
	)

	tc.EnsureResourceExists(
		odhControllerDeployment,
		WithCondition(mlflowEnvMatcher("false")),
		WithCustomErrorMsg("MLFLOW_ENABLED should be 'false' when MLflowOperator is Removed"),
	)

	tc.UpdateComponentStateInDataScienceClusterWithKind(operatorv1.Managed, componentApi.MLflowOperatorKind)

	tc.EnsureResourceExists(
		odhControllerDeployment,
		WithCondition(mlflowEnvMatcher("true")),
		WithCustomErrorMsg("MLFLOW_ENABLED should be 'true' when MLflowOperator is Managed"),
	)

	tc.UpdateComponentStateInDataScienceClusterWithKind(operatorv1.Removed, componentApi.MLflowOperatorKind)

	tc.EnsureResourceExists(
		odhControllerDeployment,
		WithCondition(mlflowEnvMatcher("false")),
		WithCustomErrorMsg("MLFLOW_ENABLED should return to 'false' when MLflowOperator is Removed again"),
	)
}

// ValidateComponentDisabled ensures module resources are removed when workbenches is disabled.
func (tc *WorkbenchesTestCtx) ValidateComponentDisabled(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke, Tier1)

	tc.EnsureResourcesExist(WithMinimalObject(tc.GVK, tc.NamespacedName))

	tc.UpdateComponentState(operatorv1.Removed)

	tc.EnsureResourceGone(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Namespace: tc.AppsNamespace,
			Name:      workbenchesModule.ControllerDeploymentName,
		}),
	)

	tc.EnsureResourcesGone(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace}),
		WithListOptions(
			&client.ListOptions{
				Namespace: tc.AppsNamespace,
				LabelSelector: k8slabels.Set{
					labels.PlatformPartOf: strings.ToLower(tc.GVK.Kind),
				}.AsSelector(),
			},
		),
		WithEventuallyTimeout(tc.TestTimeouts.componentReadinessTimeout),
	)

	tc.EnsureResourceGone(WithMinimalObject(tc.GVK, tc.NamespacedName))

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(
			And(
				jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, componentApi.WorkbenchesKind, metav1.ConditionFalse),
				jq.Match(`.status.conditions[] | select(.type == "%sReady") | .reason == "%s"`, componentApi.WorkbenchesKind, status.RemovedReason),
			),
		),
		WithCustomErrorMsg("DataScienceCluster should have %sReady condition set to False/Removed", componentApi.WorkbenchesKind),
	)
}

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

	exists, err := cluster.HasCRD(context.Background(), tc.Client(), gvk.ImageStream)
	require.NoError(t, err)
	if !exists {
		t.Skip("Skipping ImageStreamsAvailable test: ImageStream CRD not installed (vanilla K8s)")
	}

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Workbenches, types.NamespacedName{Name: componentApi.WorkbenchesInstanceName}),
		WithCondition(jq.Match(`[.status.conditions[] | select(.type == "ImageStreamsAvailable")] | length > 0`)),
	)
}
