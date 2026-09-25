package e2e_test

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
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
		WithCondition(And(
			jq.Match(`.status.readyReplicas >= 1`),
			jq.Match(
				`.spec.template.spec.containers[] | select(.name == "manager")`+
					` | .env[] | select(.name == "ENABLE_MLFLOW_OPERATOR_MODULE_CONTROLLER") | .value == "true"`,
			),
			jq.Match(
				`.spec.template.spec.containers[] | select(.name == "manager")`+
					` | .env[] | select(.name == "APPLICATIONS_NAMESPACE") | .value == "%s"`,
				tc.AppsNamespace,
			),
		)),
		WithCustomErrorMsg(
			"mlflow-operator-controller-manager should be ready in %s with module-controller env",
			tc.AppsNamespace,
		),
	)
}

func (tc *MLflowOperatorTestCtx) ValidateModuleReleases(t *testing.T) {
	t.Helper()

	tc.SkipIfXKSCluster(t)

	skipUnless(t, Smoke)

	g := NewWithT(t)
	g.Eventually(func(g Gomega) {
		platformVersion := tc.platformConfigVersion(g)
		g.Expect(platformVersion).NotTo(BeEmpty())

		module := &unstructured.Unstructured{}
		module.SetGroupVersionKind(tc.GVK)
		g.Expect(tc.Client().Get(context.Background(), tc.NamespacedName, module)).To(Succeed())
		moduleReleases, found, err := unstructured.NestedSlice(module.Object, "status", "releases")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(found).To(BeTrue(), "MLflowOperator status.releases should exist")
		modulePlatform, modulePlatformFound := releaseFieldsByName(moduleReleases, common.PlatformReleaseName)
		g.Expect(modulePlatformFound).To(BeTrue(), "MLflowOperator should publish the %s release", common.PlatformReleaseName)
		g.Expect(modulePlatform).To(Equal(platformVersion))

		dsc := &unstructured.Unstructured{}
		dsc.SetGroupVersionKind(gvk.DataScienceCluster)
		g.Expect(tc.Client().Get(context.Background(), tc.DataScienceClusterNamespacedName, dsc)).To(Succeed())
		dscReleases, found, err := unstructured.NestedSlice(
			dsc.Object,
			"status", "components", componentApi.MLflowOperatorComponentName, "releases",
		)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(found).To(BeTrue(), "DSC status.components.%s.releases should exist", componentApi.MLflowOperatorComponentName)
		dscPlatform, dscPlatformFound := releaseFieldsByName(dscReleases, common.PlatformReleaseName)
		g.Expect(dscPlatformFound).To(BeTrue(), "DSC should mirror the %s release", common.PlatformReleaseName)
		g.Expect(dscPlatform).To(Equal(platformVersion))
	}).
		WithTimeout(tc.TestTimeouts.longEventuallyTimeout).
		WithPolling(tc.TestTimeouts.defaultEventuallyPollInterval).
		Should(Succeed(), "ODH platformVersion should be published on the module CR and mirrored on DSC")
}

func releaseFieldsByName(releases []any, name string) (string, bool) {
	for _, item := range releases {
		release, ok := item.(map[string]any)
		if !ok || release["name"] != name {
			continue
		}
		version, _ := release["version"].(string)
		return version, true
	}
	return "", false
}

func (tc *MLflowOperatorTestCtx) ValidateDSCMLflowOperatorReady(t *testing.T) {
	t.Helper()

	tc.SkipIfXKSCluster(t)

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

func (tc *MLflowOperatorTestCtx) platformConfigNN() types.NamespacedName {
	return types.NamespacedName{
		Name:      modules.PlatformConfigName(componentApi.MLflowOperatorComponentName),
		Namespace: tc.AppsNamespace,
	}
}

func (tc *MLflowOperatorTestCtx) platformConfigVersion(g Gomega) string {
	cm := &corev1.ConfigMap{}
	g.Expect(tc.Client().Get(context.Background(), tc.platformConfigNN(), cm)).To(Succeed())
	g.Expect(cm.Data).To(HaveKey(modules.PlatformVersionKey))
	return cm.Data[modules.PlatformVersionKey]
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
