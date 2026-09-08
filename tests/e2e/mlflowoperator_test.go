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

	. "github.com/onsi/gomega"
)

type MLflowOperatorTestCtx struct {
	*ComponentTestCtx
}

const (
	mlflowValidateEnabledName              = "Validate component enabled"
	mlflowValidateModuleOperatorDeployName = "Validate module operator deployment"
	mlflowValidateModuleReleasesName       = "Validate module releases"
	mlflowValidateDSCReadyName             = "Validate DSC MLflowOperatorReady condition"
	mlflowValidateDisabledName             = "Validate component disabled"

	// Matches internal/controller/modules/mlflowoperator/handler.go DeploymentName.
	mlflowModuleOperatorDeployment = "mlflow-operator-controller-manager"
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

	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, tc.NamespacedName),
		WithCondition(
			jq.Match(`[.status.releases[]? | select(.name != "" and .version != "" and .repoUrl != "")] | length > 0`),
		),
		WithCustomErrorMsg("MLflowOperator CR should expose non-empty status.releases entries"),
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
