package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const (
	rayModuleControllerDeployment = "ray-module-operator-controller-manager"
	rayModuleServiceAccount       = "ray-module-operator-controller-manager"
	rayModuleClusterRoleBinding   = "ray-module-operator-manager-rolebinding"
	rayClusterCRDName             = "rayclusters.ray.io"
)

func rayTestSuite(t *testing.T) {
	t.Helper()

	tc, err := NewTestContext(t)
	require.NoError(t, err)

	moduleGVK := schema.GroupVersionKind{
		Group:   componentApi.GroupVersion.Group,
		Version: componentApi.GroupVersion.Version,
		Kind:    componentApi.RayKind,
	}
	moduleCRNN := types.NamespacedName{Name: componentApi.RayInstanceName}
	controllerNN := types.NamespacedName{
		Namespace: tc.AppsNamespace,
		Name:      rayModuleControllerDeployment,
	}

	testCases := []TestCase{
		{"Validate component enabled", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Smoke, Tier1)

			if !tc.IsXKS() {
				tc.EventuallyResourcePatched(
					WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
					WithMutateFunc(testf.Transform(`.spec.components.ray.managementState = "Removed"`)),
					WithCondition(jq.Match(`.spec.components.ray.managementState == "Removed"`)),
				)
				tc.EnsureResourceGone(WithMinimalObject(moduleGVK, moduleCRNN))
			}

			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithMutateFunc(testf.Transform(`.spec.components.ray.managementState = "Managed"`)),
				WithCondition(jq.Match(`.spec.components.ray.managementState == "Managed"`)),
			)

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
				WithCondition(jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, componentApi.RayKind, metav1.ConditionTrue)),
				WithCustomErrorMsg("DataScienceCluster should have %sReady condition set to True", componentApi.RayKind),
			)
		}},
		{"Validate module operator bootstrap resources", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Tier1)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.ServiceAccount, types.NamespacedName{
					Namespace: tc.AppsNamespace,
					Name:      rayModuleServiceAccount,
				}),
				WithCustomErrorMsg("ServiceAccount %s should exist in %s", rayModuleServiceAccount, tc.AppsNamespace),
			)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.ClusterRoleBinding, types.NamespacedName{
					Name: rayModuleClusterRoleBinding,
				}),
				WithCustomErrorMsg("ClusterRoleBinding %s should exist", rayModuleClusterRoleBinding),
			)
		}},
		{"Validate env var injection", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Tier1)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.Deployment, controllerNN),
				WithCondition(jq.Match(
					`.spec.template.spec.containers[] | select(.env != null) | .env[] | select(.name == "APPLICATIONS_NAMESPACE") | .value == "%s"`,
					tc.AppsNamespace,
				)),
				WithCustomErrorMsg("Module operator Deployment should have APPLICATIONS_NAMESPACE=%s injected", tc.AppsNamespace),
			)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.Deployment, controllerNN),
				WithCondition(jq.Match(
					`.spec.template.spec.containers[] | select(.env != null) | .env[] | select(.name == "RELATED_IMAGE_ODH_KUBERAY_OPERATOR_CONTROLLER_IMAGE") | .value != null and .value != ""`,
				)),
				WithCustomErrorMsg("Module operator Deployment should have RELATED_IMAGE_ODH_KUBERAY_OPERATOR_CONTROLLER_IMAGE injected"),
			)
		}},
		{"Validate module handler projects DSC config to Module CR", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Tier1)

			tc.EnsureResourceExists(
				WithMinimalObject(moduleGVK, moduleCRNN),
				WithCondition(And(
					jq.Match(`.spec.managementState == null`),
					jq.Match(`.spec.applicationsNamespace == "%s"`, tc.AppsNamespace),
					jq.Match(`.metadata.ownerReferences[0].kind == "DataScienceCluster"`),
				)),
			)
		}},
		{"Validate releases mirrored to DSC", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Tier1)

			tc.EnsureResourceExists(
				WithMinimalObject(moduleGVK, moduleCRNN),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(jq.Match(`.status.releases | length > 0`)),
				WithCustomErrorMsg("Ray module CR should have releases in status"),
			)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(jq.Match(`.status.components.ray.releases | length > 0`)),
				WithCustomErrorMsg("DSC status.components.ray.releases should be mirrored from module CR"),
			)
		}},
		{"Validate module CR deletion recovery", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Tier1)

			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithMutateFunc(testf.Transform(`.spec.components.ray.managementState = "Managed"`)),
				WithCondition(jq.Match(`.spec.components.ray.managementState == "Managed"`)),
			)

			tc.EnsureResourceDeletedThenRecreated(WithMinimalObject(moduleGVK, moduleCRNN))

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithCondition(jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, componentApi.RayKind, metav1.ConditionTrue)),
				WithCustomErrorMsg("DSC RayReady should recover to True after module CR recreation"),
			)
		}},
		{"Validate module operator Deployment deletion recovery", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Tier1)

			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithMutateFunc(testf.Transform(`.spec.components.ray.managementState = "Managed"`)),
				WithCondition(jq.Match(`.spec.components.ray.managementState == "Managed"`)),
			)

			tc.EnsureResourceDeletedThenRecreated(WithMinimalObject(gvk.Deployment, controllerNN))

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.Deployment, controllerNN),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(jq.Match(`.status.readyReplicas >= 1`)),
				WithCustomErrorMsg("Module operator Deployment should be recreated and reach Available after deletion"),
			)
		}},
		{"Validate component disabled", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Smoke, Tier1)

			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithMutateFunc(testf.Transform(`.spec.components.ray.managementState = "Removed"`)),
				WithCondition(jq.Match(`.spec.components.ray.managementState == "Removed"`)),
			)

			tc.EnsureResourceGone(WithMinimalObject(moduleGVK, moduleCRNN))
			tc.EnsureResourceGone(WithMinimalObject(gvk.Deployment, controllerNN))

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithCondition(And(
					jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, componentApi.RayKind, metav1.ConditionFalse),
					jq.Match(`.status.conditions[] | select(.type == "%sReady") | .reason == "%s"`, componentApi.RayKind, status.RemovedReason),
				)),
				WithCustomErrorMsg("DataScienceCluster should have %sReady condition set to False/Removed", componentApi.RayKind),
			)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{
					Name: rayClusterCRDName,
				}),
				WithCustomErrorMsg("RayCluster CRD should NOT be deleted on disable — user workload data depends on it"),
			)
		}},
	}

	RunTestCases(t, testCases)
}
