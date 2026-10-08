package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const (
	aihubModuleOperatorDeployment = "aihub-controller-manager"
	aihubModuleCRName             = "default-aihub"
	aihubTestNamespace            = "e2e-model-registries"
	aiHubReadyCondition           = "AIHubReady"
)

func aihubTestSuite(t *testing.T) {
	t.Helper()

	tc, err := NewTestContext(t)
	require.NoError(t, err)

	moduleGVK := gvk.AIHub
	moduleCRNN := types.NamespacedName{Name: aihubModuleCRName}
	controllerNN := types.NamespacedName{
		Namespace: tc.AppsNamespace,
		Name:      aihubModuleOperatorDeployment,
	}
	registriesNSNN := types.NamespacedName{Name: aihubTestNamespace}

	var originalInstancesNamespace string

	testCases := []TestCase{
		{"Validate component enabled", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Smoke, Tier1)

			// Capture the original instancesNamespace so cleanup can restore it.
			if dsc := tc.FetchDataScienceCluster(); dsc != nil {
				originalInstancesNamespace = dsc.Spec.Components.AIHub.InstancesNamespace
			}

			// Patch DSC to Managed with a non-default instancesNamespace.
			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithMutateFunc(func(obj *unstructured.Unstructured) error {
					if err := unstructured.SetNestedField(obj.Object, "Managed", "spec", "components", "aiHub", "managementState"); err != nil {
						return err
					}
					return unstructured.SetNestedField(obj.Object, aihubTestNamespace, "spec", "components", "aiHub", "instancesNamespace")
				}),
				WithCondition(And(
					jq.Match(`.spec.components.aiHub.managementState == "Managed"`),
					jq.Match(`.spec.components.aiHub.instancesNamespace == "%s"`, aihubTestNamespace),
				)),
			)

			// Assert AIHub CR Ready + ProvisioningSucceeded.
			tc.EnsureResourceExists(
				WithMinimalObject(moduleGVK, moduleCRNN),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(And(
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, metav1.ConditionTrue),
				)),
			)

			// Assert module operator Deployment available.
			tc.EnsureResourceExists(
				WithMinimalObject(gvk.Deployment, controllerNN),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(jq.Match(`.status.readyReplicas >= 1`)),
			)

			// Assert DSC AIHubReady True + component managementState Managed.
			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(And(
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, aiHubReadyCondition, metav1.ConditionTrue),
					jq.Match(`.status.components.aiHub.managementState == "Managed"`),
					jq.Match(`.status.components.aiHub.instancesNamespace == "%s"`, aihubTestNamespace),
					jq.Match(`(.status.components.aiHub | has("applicationNamespace")) == false`),
				)),
				WithCustomErrorMsg("DataScienceCluster should have %s condition set to True with Managed state", aiHubReadyCondition),
			)
		}},
		{"Validate v2 DSC AIHub selection", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Tier3)
			if tc.IsXKS() {
				t.Skip("v2 DSC conversion smoke is not supported on XKS")
			}

			namespace := &corev1.Namespace{}
			err := tc.Client().Get(t.Context(), registriesNSNN, namespace)
			if k8serr.IsNotFound(err) {
				t.Cleanup(func() {
					tc.DeleteResource(WithMinimalObject(gvk.Namespace, registriesNSNN), WithIgnoreNotFound(true), WithWaitForDeletion(true))
				})
			} else {
				require.NoError(t, err)
			}

			snapshotV2DSCFields(t, tc, []string{"spec", "components", "modelregistry"})
			// The namespace is immutable while Managed. This cleanup runs before
			// the snapshot restoration so an unrelated original value can be restored.
			t.Cleanup(func() {
				tc.EventuallyResourcePatched(
					WithMinimalObject(gvk.DataScienceClusterV2, tc.DataScienceClusterNamespacedName),
					WithMutateFunc(testf.Transform(`.spec.components.modelregistry.managementState = "Removed"`)),
					WithCondition(jq.Match(`.spec.components.modelregistry.managementState == "Removed"`)),
				)
			})

			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.DataScienceClusterV2, tc.DataScienceClusterNamespacedName),
				WithMutateFunc(testf.TransformPipeline(
					testf.Transform(`.spec.components.modelregistry.managementState = "Removed"`),
					testf.Transform(`.spec.components.modelregistry.registriesNamespace = "%s"`, aihubTestNamespace),
				)),
			)
			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.DataScienceClusterV2, tc.DataScienceClusterNamespacedName),
				WithMutateFunc(testf.Transform(`.spec.components.modelregistry.managementState = "Managed"`)),
				WithCondition(And(
					jq.Match(`.spec.components.modelregistry.managementState == "Managed"`),
					jq.Match(`.spec.components.modelregistry.registriesNamespace == "%s"`, aihubTestNamespace),
				)),
			)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithCondition(And(
					jq.Match(`.spec.components.aiHub.managementState == "Managed"`),
					jq.Match(`.spec.components.aiHub.instancesNamespace == "%s"`, aihubTestNamespace),
				)),
			)
			tc.EnsureResourceExists(
				WithMinimalObject(moduleGVK, moduleCRNN),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(And(
					jq.Match(`.spec.instancesNamespace == "%s"`, aihubTestNamespace),
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
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
				WithCondition(And(
					jq.Match(`.status.components.aiHub.managementState == "Managed"`),
					jq.Match(`.status.components.aiHub.instancesNamespace == "%s"`, aihubTestNamespace),
					jq.Match(`(.status.components.aiHub | has("applicationNamespace")) == false`),
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, aiHubReadyCondition, metav1.ConditionTrue),
				)),
			)
			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceClusterV2, tc.DataScienceClusterNamespacedName),
				WithCondition(And(
					jq.Match(`.spec.components.modelregistry.registriesNamespace == "%s"`, aihubTestNamespace),
					jq.Match(`.status.components.modelregistry.registriesNamespace == "%s"`, aihubTestNamespace),
					jq.Match(`.status.conditions[] | select(.type == "ModelRegistryReady") | .status == "True"`),
				)),
			)
		}},
		{"Validate module CR spec", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Tier1)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.AIHub, moduleCRNN),
				WithCondition(And(
					jq.Match(`.spec.applicationNamespace == "%s"`, tc.AppsNamespace),
					jq.Match(`.spec.instancesNamespace == "%s"`, aihubTestNamespace),
				)),
			)
		}},
		{"Validate registries namespace created", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Tier1)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.Namespace, registriesNSNN),
				WithCustomErrorMsg("non-default registries namespace %s should be auto-created", aihubTestNamespace),
			)
		}},
		{"Validate releases mirrored to DSC", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Tier1)

			tc.EnsureResourceExists(
				WithMinimalObject(moduleGVK, moduleCRNN),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(jq.Match(`.status.releases | length > 0`)),
				WithCustomErrorMsg("AIHub module CR should have releases in status"),
			)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(jq.Match(`.status.components.aiHub.releases | length > 0`)),
				WithCustomErrorMsg("DSC status.components.aiHub.releases should be mirrored from module CR"),
			)
		}},
		{"Validate component disabled", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Smoke, Tier1)

			// Transition AI Hub to Removed and restore instancesNamespace in one patch.
			// The CEL immutability rule allows changing instancesNamespace only when
			// managementState is being set to Removed in the same mutation.
			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithMutateFunc(func(obj *unstructured.Unstructured) error {
					if err := unstructured.SetNestedField(obj.Object, "Removed", "spec", "components", "aiHub", "managementState"); err != nil {
						return err
					}
					return unstructured.SetNestedField(obj.Object, originalInstancesNamespace, "spec", "components", "aiHub", "instancesNamespace")
				}),
				WithCondition(And(
					jq.Match(`.spec.components.aiHub.managementState == "Removed"`),
					jq.Match(`.spec.components.aiHub.instancesNamespace == "%s"`, originalInstancesNamespace),
				)),
			)

			tc.EnsureResourceGone(WithMinimalObject(moduleGVK, moduleCRNN))
			tc.EnsureResourceGone(WithMinimalObject(gvk.Deployment, controllerNN))

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithCondition(And(
					jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, aiHubReadyCondition, metav1.ConditionFalse),
					jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, aiHubReadyCondition, status.RemovedReason),
				)),
				WithCustomErrorMsg("DataScienceCluster should have %s condition set to False/Removed", aiHubReadyCondition),
			)

			// Clean up the test namespace.
			tc.DeleteResource(
				WithMinimalObject(gvk.Namespace, registriesNSNN),
				WithIgnoreNotFound(true),
				WithWaitForDeletion(true),
			)
		}},
	}

	RunTestCases(t, testCases)
}
