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
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	workbenchesModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/workbenches"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

type WorkbenchesTestCtx struct {
	*ComponentTestCtx
}

func workbenchesTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewModuleTestCtx(t, workbenchesModule.NewHandler())
	require.NoError(t, err)

	componentCtx := WorkbenchesTestCtx{
		ComponentTestCtx: ct,
	}

	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate module enabled", componentCtx.ValidateModuleEnabled},
		{"Validate module operator deployment", componentCtx.ValidateModuleOperatorDeployment},
		{"Validate workbenches namespace configuration", componentCtx.ValidateWorkbenchesNamespaceConfiguration},
		{"Validate ingress projection", componentCtx.ValidateIngressProjection},
		{"Validate module releases", componentCtx.ValidateModuleReleases},
		{"Validate ImageStreams available", componentCtx.ValidateImageStreamsAvailable},
		{"Validate MLflow integration", componentCtx.ValidateMLflowIntegration},
		{"Validate WorkbenchesV2 default Removed", componentCtx.ValidateWorkbenchesV2DefaultRemoved},
		{"Validate resource deletion recovery", componentCtx.ValidateAllDeletionRecovery},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
		{"Validate module disabled", componentCtx.ValidateModuleDisabled},
		{"Validate WorkbenchesV2 when parent disabled", componentCtx.ValidateWorkbenchesV2ParentDisabled},
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

	// Operands deploy into ApplicationsNamespace (APPLICATIONS_NAMESPACE).
	// spec/status.workbenchNamespace are the legacy DSC notebooks NS; status
	// reports the active operand namespace in status.applicationsNamespace.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: tc.AppsNamespace}),
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
				jq.Match(`.status.applicationsNamespace == "%s"`, tc.AppsNamespace),
			),
		),
		WithCustomErrorMsg(
			"Workbenches CR should echo legacy workbenchNamespace=%s and report applicationsNamespace=%s",
			tc.WorkbenchesNamespace,
			tc.AppsNamespace,
		),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      "odh-notebook-controller-manager",
			Namespace: tc.AppsNamespace,
		}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "Available") | .status == "True"`)),
		WithCustomErrorMsg("notebook controller should be deployed in applications namespace %s", tc.AppsNamespace),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(`.status.components.workbenches.workbenchNamespace == "%s"`, tc.WorkbenchesNamespace)),
	)
}

func (tc *WorkbenchesTestCtx) ValidateIngressProjection(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier1)

	configKey := types.NamespacedName{Name: serviceApi.GatewayConfigName}
	config := &serviceApi.GatewayConfig{}
	require.NoError(t, tc.Client().Get(tc.Context(), configKey, config))
	require.NotEmpty(t, config.Status.Domain)
	original := config.DeepCopy()

	validate := func(additional serviceApi.AdditionalIngresses) {
		conditions := make([]OmegaMatcher, 0, len(additional)+2)
		conditions = append(conditions,
			jq.Match(`.spec.ingresses | length == %d`, len(additional)+1),
			jq.Match(`.spec.ingresses | map(select(.isDefault == true)) == [{
				name: "%s", gatewayName: "%s", gatewayNamespace: "%s", hostname: "%s", isDefault: true
			}]`, gateway.GetDefaultGatewayName(), gateway.GetDefaultGatewayName(), gateway.GetGatewayNamespace(), original.Status.Domain),
		)
		for _, ingress := range additional {
			conditions = append(conditions, jq.Match(`.spec.ingresses | map(select(.name == "%s")) == [{
				name: "%s", gatewayName: "%s", gatewayNamespace: "%s", hostname: "%s"
			}]`, ingress.Name, ingress.Name, ingress.Name, gateway.GetGatewayNamespace(), ingress.Hostname))
		}
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.Workbenches, types.NamespacedName{Name: componentApi.WorkbenchesInstanceName}),
			WithCondition(And(conditions...)),
			WithCustomErrorMsg("Workbenches should project the default and %d additional ingresses", len(additional)),
		)
	}
	validate(original.Spec.AdditionalIngresses)

	if config.Spec.IngressMode != serviceApi.IngressModeOcpRoute {
		t.Log("Additional ingress lifecycle requires OcpRoute mode; default projection validated")
		return
	}
	tc.SkipIfBYOIDC(t)

	update := func(additional serviceApi.AdditionalIngresses) error {
		return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			current := &serviceApi.GatewayConfig{}
			if err := tc.Client().Get(tc.Context(), configKey, current); err != nil {
				return err
			}
			current.Spec.AdditionalIngresses = additional
			return tc.Client().Update(tc.Context(), current)
		})
	}
	t.Cleanup(func() {
		if err := update(original.Spec.AdditionalIngresses); err != nil {
			t.Errorf("failed to restore GatewayConfig additional ingresses: %v", err)
			return
		}
		validate(original.Spec.AdditionalIngresses)
	})

	additional := serviceApi.AdditionalIngresses{{
		Name: "e2e-workbenches", Hostname: "workbenches.e2e.invalid",
		IngressControllerName: "e2e-workbenches-missing",
		RouteLabels:           map[string]string{"example.com/ingress": "e2e-workbenches"},
	}}
	require.NoError(t, update(additional))
	// Published assignments must reach Workbenches even when the ingress is not ready.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.GatewayConfig, configKey),
		WithCondition(jq.Match(`.status.additionalIngresses[] | select(.name == "%s") |
			.gatewayRef == {name: "%s", namespace: "%s"} and
			any(.conditions[]; .type == "Ready" and .status == "False")`,
			additional[0].Name, additional[0].Name, gateway.GetGatewayNamespace())),
	)
	validate(additional)

	additional[0].Hostname = "updated-workbenches.e2e.invalid"
	require.NoError(t, update(additional))
	validate(additional)

	require.NoError(t, update(nil))
	validate(nil)
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

	// Operands are reconciled into ApplicationsNamespace (APPLICATIONS_NAMESPACE),
	// not the legacy DSC workbenchNamespace field.
	odhControllerDeployment := WithMinimalObject(gvk.Deployment, types.NamespacedName{
		Name:      odhNotebookControllerManager,
		Namespace: tc.AppsNamespace,
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

// ValidateWorkbenchesV2DefaultRemoved checks that the default workbenchesV2: Removed
// fixture is projected into the module CR and mirrored as a Removed DSC condition.
func (tc *WorkbenchesTestCtx) ValidateWorkbenchesV2DefaultRemoved(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Workbenches, types.NamespacedName{Name: componentApi.WorkbenchesInstanceName}),
		WithCondition(jq.Match(`.spec.workbenchesV2.managementState == "Removed"`)),
		WithCustomErrorMsg("Workbenches module CR should project workbenchesV2.managementState=Removed"),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithCondition(
			And(
				jq.Match(`.status.conditions[] | select(.type == "WorkbenchesV2Ready") | .status == "False"`),
				jq.Match(`.status.conditions[] | select(.type == "WorkbenchesV2Ready") | .reason == "%s"`, status.RemovedReason),
				jq.Match(`.status.components.workbenchesV2.managementState == "Removed"`),
			),
		),
		WithCustomErrorMsg("DSC WorkbenchesV2Ready should be False/Removed when workbenchesV2 is Removed"),
	)
}

// ValidateWorkbenchesV2ParentDisabled checks that disabling the parent Workbenches
// module forces WorkbenchesV2 to Removed on the DSC, even when the submodule was Managed.
func (tc *WorkbenchesTestCtx) ValidateWorkbenchesV2ParentDisabled(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)

	// ValidateComponentDisabled leaves workbenches Removed; re-enable to exercise the
	// Managed -> Removed transition when the parent module is disabled.
	tc.UpdateComponentState(operatorv1.Managed)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Workbenches, types.NamespacedName{Name: componentApi.WorkbenchesInstanceName}),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue)),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Namespace: tc.AppsNamespace,
			Name:      workbenchesModule.ControllerDeploymentName,
		}),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithCondition(jq.Match(`.status.readyReplicas >= 1`)),
	)

	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.workbenches.workbenchesV2.managementState = "Managed"`)),
		WithCondition(jq.Match(`.spec.components.workbenches.workbenchesV2.managementState == "Managed"`)),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Workbenches, types.NamespacedName{Name: componentApi.WorkbenchesInstanceName}),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithCondition(jq.Match(`.spec.workbenchesV2.managementState == "Managed"`)),
		WithCustomErrorMsg("Workbenches module CR should project workbenchesV2.managementState=Managed"),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithCondition(jq.Match(`.status.components.workbenchesV2.managementState == "Managed"`)),
		WithCustomErrorMsg("DSC status.components.workbenchesV2 should be Managed before parent is disabled"),
	)

	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.workbenches.managementState = "Removed"`)),
		WithCondition(jq.Match(`.spec.components.workbenches.managementState == "Removed"`)),
	)

	// Cleanup must complete before ComputeModulesStatus updates submodule conditions.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithCondition(
			And(
				jq.Match(`.status.conditions[] | select(.type == "WorkbenchesV2Ready") | .status == "False"`),
				jq.Match(`.status.conditions[] | select(.type == "WorkbenchesV2Ready") | .reason == "%s"`, status.RemovedReason),
				jq.Match(`.status.components.workbenchesV2.managementState == "Removed"`),
			),
		),
		WithCustomErrorMsg("WorkbenchesV2Ready should show Removed when parent workbenches is disabled"),
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
