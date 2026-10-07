package e2e_test

import (
	"testing"

	gTypes "github.com/onsi/gomega/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	aigatewayModule "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/aigateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const aiGatewayControllerDeployment = "ai-gateway-operator"

func aiGatewayTestSuite(t *testing.T) {
	t.Helper()

	tc, err := NewTestContext(t)
	require.NoError(t, err)

	moduleGVK := schema.GroupVersionKind{
		Group:   componentApi.GroupVersion.Group,
		Version: componentApi.GroupVersion.Version,
		Kind:    componentApi.AIGatewayKind,
	}
	moduleCRNN := types.NamespacedName{Name: componentApi.AIGatewayInstanceName}
	controllerNN := types.NamespacedName{
		Namespace: tc.AppsNamespace,
		Name:      aiGatewayControllerDeployment,
	}
	relatedImageEnvVars := aigatewayModule.NewHandler().GetRelatedImages()

	testCases := []TestCase{
		{"Validate component enabled", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Smoke, Tier1)

			if !tc.IsXKS() {
				tc.EventuallyResourcePatched(
					WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
					WithMutateFunc(testf.Transform(`.spec.components.aigateway.managementState = "Removed"`)),
					WithCondition(jq.Match(`.spec.components.aigateway.managementState == "Removed"`)),
				)
				tc.EnsureResourceGone(WithMinimalObject(moduleGVK, moduleCRNN))
			}

			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithMutateFunc(testf.Transform(`.spec.components.aigateway.managementState = "Managed"`)),
				WithCondition(jq.Match(`.spec.components.aigateway.managementState == "Managed"`)),
			)

			// Removing then re-enabling requires a full AGO re-deploy (image pull included);
			// use the long timeout to accommodate slower environments.
			tc.EnsureResourceExists(
				WithMinimalObject(moduleGVK, moduleCRNN),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(And(
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, metav1.ConditionTrue),
				)),
			)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.Deployment, controllerNN),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(jq.Match(`.status.readyReplicas >= 1`)),
			)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, componentApi.AIGatewayKind, metav1.ConditionTrue)),
				WithCustomErrorMsg("DataScienceCluster should have %sReady condition set to True", componentApi.AIGatewayKind),
			)
		}},
		{"Validate env var injection", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Tier1)
			validateAIGatewayEnvVarInjection(t, tc, controllerNN, relatedImageEnvVars)
		}},
		{"Validate releases mirrored to DSC", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Tier1)

			// Module CR should have releases populated by the module operator.
			tc.EnsureResourceExists(
				WithMinimalObject(moduleGVK, moduleCRNN),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(jq.Match(`.status.releases | length > 0`)),
				WithCustomErrorMsg("AIGateway module CR should have releases in status"),
			)

			// DSC should mirror the module CR's releases.
			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(jq.Match(`.status.components.aigateway.releases | length > 0`)),
				WithCustomErrorMsg("DSC status.components.aigateway.releases should be mirrored from module CR"),
			)
		}},
		{"Validate module CR deletion recovery", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Tier1)

			// Ensure AIGateway is enabled before attempting deletion.
			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithMutateFunc(testf.Transform(`.spec.components.aigateway.managementState = "Managed"`)),
				WithCondition(jq.Match(`.spec.components.aigateway.managementState == "Managed"`)),
			)

			// EnsureResourceDeletedThenRecreated handles the full delete→recreation cycle:
			// captures original UID, deletes, waits for deletion acknowledgment, then waits
			// for the platform reconciler to recreate with a new UID.
			tc.EnsureResourceDeletedThenRecreated(WithMinimalObject(moduleGVK, moduleCRNN))

			// Verify DSC recovers after recreation.
			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithCondition(jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, componentApi.AIGatewayKind, metav1.ConditionTrue)),
				WithCustomErrorMsg("DSC AIGatewayReady should recover to True after module CR recreation"),
			)
		}},
		{"Validate submodule conditions with mixed enablement", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Tier1)

			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithMutateFunc(testf.Transform(`.spec.components.aigateway.managementState = "Managed"`)),
				WithCondition(jq.Match(`.spec.components.aigateway.managementState == "Managed"`)),
			)

			// Enable BatchGateway, disable ModelsAsAService.
			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithMutateFunc(testf.TransformPipeline(
					testf.Transform(`.spec.components.aigateway.batchGateway.managementState = "Managed"`),
					testf.Transform(`.spec.components.aigateway.modelsAsAService.managementState = "Removed"`),
				)),
				WithCondition(And(
					jq.Match(`.spec.components.aigateway.batchGateway.managementState == "Managed"`),
					jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Removed"`),
				)),
			)

			tc.EnsureResourceExists(
				WithMinimalObject(moduleGVK, moduleCRNN),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue)),
			)

			// ModelsAsAServiceReady should be False/Removed since the submodule is disabled.
			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(And(
					jq.Match(`.status.conditions[] | select(.type == "ModelsAsAServiceReady") | .status == "False"`),
					jq.Match(`.status.conditions[] | select(.type == "ModelsAsAServiceReady") | .reason == "%s"`, status.RemovedReason),
				)),
				WithCustomErrorMsg("DSC ModelsAsAServiceReady should be False/Removed when submodule is disabled"),
			)

			// BatchGatewayReady should exist on the DSC (mirrored from the module CR).
			// The condition may be True if the operator has reported it, or False/AwaitingReadiness
			// if the operator hasn't reported yet — either way, it must be present.
			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(jq.Match(`.status.conditions[] | select(.type == "BatchGatewayReady") | .type == "BatchGatewayReady"`)),
				WithCustomErrorMsg("DSC BatchGatewayReady condition should be present when submodule is Managed"),
			)

			// Verify DSC status.components reflects the correct managementState for each submodule.
			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithCondition(And(
					jq.Match(`.status.components.modelsAsAService.managementState == "Removed"`),
					jq.Match(`.status.components.batchGateway.managementState == "Managed"`),
					jq.Match(`.status.components.aigateway.managementState == "Managed"`),
				)),
				WithCustomErrorMsg("DSC status.components should reflect submodule managementState"),
			)
		}},
		{"Validate submodule conditions when parent disabled", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Tier1)

			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithMutateFunc(testf.Transform(`.spec.components.aigateway.managementState = "Removed"`)),
				WithCondition(jq.Match(`.spec.components.aigateway.managementState == "Removed"`)),
			)

			// When parent module is Removed, all submodule conditions must also show Removed.
			// Cleanup must complete (CR finalizer + operator scale-down) before ComputeModulesStatus
			// updates the DSC conditions, so use the long timeout.
			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(And(
					jq.Match(`.status.conditions[] | select(.type == "ModelsAsAServiceReady") | .status == "False"`),
					jq.Match(`.status.conditions[] | select(.type == "ModelsAsAServiceReady") | .reason == "%s"`, status.RemovedReason),
					jq.Match(`.status.conditions[] | select(.type == "BatchGatewayReady") | .status == "False"`),
					jq.Match(`.status.conditions[] | select(.type == "BatchGatewayReady") | .reason == "%s"`, status.RemovedReason),
				)),
				WithCustomErrorMsg("All submodule conditions should show Removed when parent AIGateway is disabled"),
			)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(jq.Match(`.status.components.aigateway.managementState == "Removed"`)),
				WithCustomErrorMsg("DSC status.components.aigateway should show Removed"),
			)
		}},
		{"Validate v2 DSC canonical MaaS selection", func(t *testing.T) {
			t.Helper()
			validateV2DSCCanonicalMaaSSelection(t, tc, moduleGVK, moduleCRNN, controllerNN)
		}},
		{"Validate component disabled", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Smoke, Tier1)

			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithMutateFunc(testf.Transform(`.spec.components.aigateway.managementState = "Removed"`)),
				WithCondition(jq.Match(`.spec.components.aigateway.managementState == "Removed"`)),
			)

			tc.EnsureResourceGone(WithMinimalObject(moduleGVK, moduleCRNN))
			tc.EnsureResourceGone(WithMinimalObject(gvk.Deployment, controllerNN))

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithCondition(And(
					jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, componentApi.AIGatewayKind, metav1.ConditionFalse),
					jq.Match(`.status.conditions[] | select(.type == "%sReady") | .reason == "%s"`, componentApi.AIGatewayKind, status.RemovedReason),
				)),
				WithCustomErrorMsg("DataScienceCluster should have %sReady condition set to False/Removed", componentApi.AIGatewayKind),
			)
		}},
	}

	RunTestCases(t, testCases)
}

func validateAIGatewayEnvVarInjection(t *testing.T, tc *TestContext, controllerNN types.NamespacedName, relatedImageEnvVars []string) {
	t.Helper()
	require.NotEmpty(t, relatedImageEnvVars, "aigateway handler should declare related images for env injection")

	relatedImageEnvVarSet := make(map[string]struct{}, len(relatedImageEnvVars))
	for _, envVarName := range relatedImageEnvVars {
		relatedImageEnvVarSet[envVarName] = struct{}{}
	}

	operatorDeploymentNN := types.NamespacedName{
		Namespace: tc.OperatorNamespace,
		Name:      tc.getControllerDeploymentName(),
	}
	operatorDeployment := &appsv1.Deployment{}
	tc.FetchTypedResource(
		operatorDeployment,
		WithMinimalObject(gvk.Deployment, operatorDeploymentNN),
		WithCustomErrorMsg("Failed to fetch operator Deployment %s in namespace %s", operatorDeploymentNN.Name, operatorDeploymentNN.Namespace),
	)

	expectedRelatedImageEnvVars := map[string]string{}
	for _, container := range operatorDeployment.Spec.Template.Spec.Containers {
		for _, envVar := range container.Env {
			if _, shouldCheck := relatedImageEnvVarSet[envVar.Name]; !shouldCheck {
				continue
			}
			if envVar.Value == "" {
				continue
			}
			expectedRelatedImageEnvVars[envVar.Name] = envVar.Value
		}
	}
	matchers := make([]gTypes.GomegaMatcher, 0, 1+len(expectedRelatedImageEnvVars))
	// The platform injects APPLICATIONS_NAMESPACE into every module operator
	// deployment unconditionally. Verify it's present with the correct value.
	matchers = append(matchers, jq.Match(
		`.spec.template.spec.containers[] | select(.env != null) | .env[] | select(.name == "APPLICATIONS_NAMESPACE") | .value == "%s"`,
		tc.AppsNamespace,
	))

	for envVarName, expectedValue := range expectedRelatedImageEnvVars {
		matchers = append(matchers, jq.Match(
			`.spec.template.spec.containers[] | select(.env != null) | .env[] | select(.name == "%s") | .value == "%s"`,
			envVarName,
			expectedValue,
		))
	}

	if len(expectedRelatedImageEnvVars) == 0 {
		t.Logf(
			"No non-empty AIGateway RELATED_IMAGE_* env vars configured on operator Deployment %s/%s; validating APPLICATIONS_NAMESPACE only",
			operatorDeploymentNN.Namespace,
			operatorDeploymentNN.Name,
		)
	}

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, controllerNN),
		WithCondition(And(matchers...)),
		WithCustomErrorMsg(
			"ai-gateway-operator Deployment should have expected env var injection from operator Deployment %s/%s",
			operatorDeploymentNN.Namespace,
			operatorDeploymentNN.Name,
		),
	)
}

func validateV2DSCCanonicalMaaSSelection(t *testing.T, tc *TestContext, moduleGVK schema.GroupVersionKind, moduleCRNN, controllerNN types.NamespacedName) {
	t.Helper()
	skipUnless(t, Tier3)
	if tc.IsXKS() {
		t.Skip("v2 DSC conversion smoke is not supported on XKS")
	}

	snapshotV2DSCFields(t, tc, []string{"spec", "components", "aigateway"})
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceClusterV2, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.components.aigateway.managementState = "Managed"`),
			testf.Transform(`.spec.components.aigateway.modelsAsAService.managementState = "Managed"`),
		)),
	)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(And(
			jq.Match(`.spec.components.aigateway.managementState == "Managed"`),
			jq.Match(`.spec.components.aigateway.modelsAsAService.managementState == "Managed"`),
		)),
	)
	tc.EnsureResourceExists(
		WithMinimalObject(moduleGVK, moduleCRNN),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue)),
	)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, controllerNN),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithCondition(jq.Match(`.status.readyReplicas >= 1`)),
	)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithCondition(And(
			jq.Match(`.status.components.aigateway.managementState == "Managed"`),
			jq.Match(`.status.components.modelsAsAService.managementState == "Managed"`),
			jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, componentApi.AIGatewayKind, metav1.ConditionTrue),
		)),
	)

	// DEC-024 preserves legacy KServe provenance: if it was already Managed,
	// the v2 read normalizes canonical MaaS back to Removed.
	v2Read := &dscv2.DataScienceCluster{}
	require.NoError(t, tc.Client().Get(t.Context(), tc.DataScienceClusterNamespacedName, v2Read))
	require.Equal(t, operatorv1.Managed, v2Read.Status.Components.ModelsAsAService.ManagementState)
	//nolint:staticcheck // v2 compatibility read.
	if v2Read.Spec.Components.Kserve.ModelsAsService.ManagementState == operatorv1.Managed {
		require.Equal(t, operatorv1.Removed, v2Read.Spec.Components.AIGateway.ModelsAsAService.ManagementState)
	} else {
		require.Equal(t, operatorv1.Managed, v2Read.Spec.Components.AIGateway.ModelsAsAService.ManagementState)
	}
}
