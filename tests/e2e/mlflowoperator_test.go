package e2e_test

import (
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

type MLflowOperatorTestCtx struct {
	*ComponentTestCtx
}

const (
	mlflowValidateEnabledName              = "Validate component enabled"
	mlflowValidateModuleOperatorDeployName = "Validate module operator deployment"
	mlflowValidateModuleReleasesName       = "Validate module releases"
	mlflowValidateDSCReadyName                  = "Validate DSC MLflowOperatorReady condition"
	mlflowValidateDisabledName                  = "Validate component disabled"
	mlflowValidateDisabledWithOperandName       = "Validate module disabled with live MLflow operand"

	// Matches internal/controller/modules/mlflowoperator/handler.go DeploymentName.
	mlflowModuleOperatorDeployment = "mlflow-operator-controller-manager"
	// Matches mlflow-operator internal/controller/mlflowoperator_controller.go mlflowReleaseName.
	mlflowModuleReleaseName = "MLflow"
	// Operand used to exercise MLflowOperator protection during DSC Removed cleanup.
	mlflowOperandInstanceName = "mlflow-e2e"
	// Matches mlflow-operator internal/controller/mlflowoperator_controller.go.
	mlflowOperatorProtectionFinalizer = "mlflow.opendatahub.io/mlflow-operator-protection"
	mlflowInstancesPresentReason      = "MLflowInstancesPresent"
)

func mlflowOperatorTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewModuleTestCtx(t, gvk.MLflowOperator, componentApi.MLflowOperatorInstanceName)
	require.NoError(t, err)

	componentCtx := MLflowOperatorTestCtx{
		ComponentTestCtx: ct,
	}

	testCases := []TestCase{
		{mlflowValidateEnabledName, componentCtx.ValidateModuleEnabled},
		{mlflowValidateModuleOperatorDeployName, componentCtx.ValidateModuleOperatorDeployment},
		{mlflowValidateModuleReleasesName, componentCtx.ValidateModuleReleases},
		{mlflowValidateDSCReadyName, componentCtx.ValidateDSCMLflowOperatorReady},
		{mlflowValidateDisabledName, componentCtx.ValidateModuleDisabled},
		{mlflowValidateDisabledWithOperandName, componentCtx.ValidateModuleDisabledWithLiveOperand},
	}

	RunTestCases(t, testCases)
}

func (tc *MLflowOperatorTestCtx) ValidateModuleEnabled(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke, Tier1)

	if !tc.IsXKS() {
		tc.UpdateComponentStateInDataScienceCluster(operatorv1.Removed)
	}

	tc.UpdateComponentState(operatorv1.Managed)

	tc.EnsureResourcesExist(
		WithMinimalObject(tc.GVK, tc.NamespacedName),
		WithCondition(
			And(
				HaveLen(1),
				HaveEach(And(
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
					jq.Match(`.spec.gatewayName != ""`),
					jq.Match(`.spec.sectionTitle != ""`),
				)),
			),
		),
	)
}

func (tc *MLflowOperatorTestCtx) ValidateModuleOperatorDeployment(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke, Tier1)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Namespace: tc.AppsNamespace,
			Name:      mlflowModuleOperatorDeployment,
		}),
		WithCondition(jq.Match(`.status.readyReplicas >= 1`)),
		WithCustomErrorMsg("mlflow-operator-controller-manager should be ready in %s", tc.AppsNamespace),
	)
}

func (tc *MLflowOperatorTestCtx) ValidateModuleReleases(t *testing.T) {
	t.Helper()

	tc.SkipIfXKSCluster(t)

	skipUnless(t, Smoke)

	mlflowReleaseChecks := And(
		jq.Match(`.status.releases[] | select(.name == "%s") | .version != ""`, mlflowModuleReleaseName),
		jq.Match(`.status.releases[] | select(.name == "%s") | .repoUrl != ""`, mlflowModuleReleaseName),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, tc.NamespacedName),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithCondition(mlflowReleaseChecks),
		WithCustomErrorMsg("MLflowOperator CR should publish the %s release with version and repoUrl", mlflowModuleReleaseName),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithCondition(And(
			jq.Match(`.status.components.%s.releases[] | select(.name == "%s") | .version != ""`,
				componentApi.MLflowOperatorComponentName, mlflowModuleReleaseName),
			jq.Match(`.status.components.%s.releases[] | select(.name == "%s") | .repoUrl != ""`,
				componentApi.MLflowOperatorComponentName, mlflowModuleReleaseName),
		)),
		WithCustomErrorMsg(
			"DSC status.components.%s.releases should mirror the %s release from the module CR",
			componentApi.MLflowOperatorComponentName,
			mlflowModuleReleaseName,
		),
	)
}

func (tc *MLflowOperatorTestCtx) ValidateDSCMLflowOperatorReady(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke, Tier1)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(
			`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`,
			componentApi.MLflowOperatorKind,
			metav1.ConditionTrue,
		)),
		WithCustomErrorMsg("DataScienceCluster should have MLflowOperatorReady condition set to True"),
	)
}

func (tc *MLflowOperatorTestCtx) ValidateModuleDisabled(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke, Tier1)

	tc.EnsureResourcesExist(WithMinimalObject(tc.GVK, tc.NamespacedName))

	tc.UpdateComponentState(operatorv1.Removed)

	tc.EnsureResourceGone(WithMinimalObject(tc.GVK, tc.NamespacedName))

	tc.EnsureResourceGone(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Namespace: tc.AppsNamespace,
			Name:      mlflowModuleOperatorDeployment,
		}),
		WithEventuallyTimeout(tc.TestTimeouts.componentReadinessTimeout),
	)
}

func (tc *MLflowOperatorTestCtx) ValidateModuleDisabledWithLiveOperand(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke, Tier1)

	controllerNN := types.NamespacedName{
		Namespace: tc.AppsNamespace,
		Name:      mlflowModuleOperatorDeployment,
	}
	mlflowNN := types.NamespacedName{Name: mlflowOperandInstanceName}

	tc.UpdateComponentState(operatorv1.Managed)

	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, tc.NamespacedName),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithCondition(jq.Match(
			`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
			status.ConditionTypeReady,
			metav1.ConditionTrue,
		)),
		WithCustomErrorMsg("MLflowOperator module CR should be Ready before creating a live MLflow operand"),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, controllerNN),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithCondition(jq.Match(`.status.readyReplicas >= 1`)),
		WithCustomErrorMsg("mlflow-operator-controller-manager should be ready before creating a live MLflow operand"),
	)

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(createE2EMLflowInstance(mlflowOperandInstanceName)),
		WithCustomErrorMsg("Failed to create MLflow operand %s", mlflowOperandInstanceName),
	)

	defer func() {
		tc.DeleteResource(
			WithMinimalObject(gvk.MLflow, mlflowNN),
			WithIgnoreNotFound(true),
			WithWaitForDeletion(true),
			WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		)
	}()

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.MLflow, mlflowNN),
		WithCustomErrorMsg("MLflow operand %s should exist before disabling the module", mlflowOperandInstanceName),
	)

	tc.UpdateComponentState(operatorv1.Removed)

	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, tc.NamespacedName),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithCondition(And(
			jq.Match(`.metadata.deletionTimestamp != null`),
			jq.Match(`.metadata.finalizers[]? | select(. == "%s")`, mlflowOperatorProtectionFinalizer),
			jq.Match(
				`.status.conditions[] | select(.type == "%s") | .reason == "%s"`,
				status.ConditionTypeReady,
				mlflowInstancesPresentReason,
			),
		)),
		WithCustomErrorMsg(
			"MLflowOperator should remain deleting with %s while MLflow operand %s exists",
			mlflowInstancesPresentReason,
			mlflowOperandInstanceName,
		),
	)

	tc.EnsureResourceExistsConsistently(
		WithMinimalObject(gvk.Deployment, controllerNN),
		WithConsistentlyDuration(30*time.Second),
		WithCondition(jq.Match(`.status.readyReplicas >= 1`)),
		WithCustomErrorMsg(
			"mlflow-operator-controller-manager should stay ready while MLflowOperator finalizes with a live MLflow operand",
		),
	)

	tc.DeleteResource(
		WithMinimalObject(gvk.MLflow, mlflowNN),
		WithWaitForDeletion(true),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
	)

	tc.EnsureResourceGone(
		WithMinimalObject(tc.GVK, tc.NamespacedName),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
	)

	tc.EnsureResourceGone(
		WithMinimalObject(gvk.Deployment, controllerNN),
		WithEventuallyTimeout(tc.TestTimeouts.componentReadinessTimeout),
	)
}

func createE2EMLflowInstance(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "mlflow.opendatahub.io/v1",
			"kind":       "MLflow",
			"metadata": map[string]interface{}{
				"name": name,
			},
			"spec": map[string]interface{}{
				"replicas": int64(1),
				"resources": map[string]interface{}{
					"requests": map[string]interface{}{
						"cpu":    "100m",
						"memory": "256Mi",
					},
					"limits": map[string]interface{}{
						"cpu":    "500m",
						"memory": "512Mi",
					},
				},
				"storage": map[string]interface{}{
					"accessModes": []interface{}{"ReadWriteOnce"},
					"resources": map[string]interface{}{
						"requests": map[string]interface{}{
							"storage": "5Gi",
						},
					},
				},
				"backendStoreUri":      "sqlite:////mlflow/mlflow.db",
				"registryStoreUri":     "sqlite:////mlflow/mlflow.db",
				"artifactsDestination": "file:///mlflow/artifacts",
				"serveArtifacts":       true,
			},
		},
	}
}
