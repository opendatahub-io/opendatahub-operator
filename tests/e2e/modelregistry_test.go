package e2e_test

import (
	"testing"

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

const (
	modelRegistryModuleOperatorDeployment = "aihub-controller-manager"
	modelRegistryModuleCRName             = "default-aihub"
	modelRegistryTestNamespace            = "e2e-model-registries"
)

func modelRegistryTestSuite(t *testing.T) {
	t.Helper()

	tc, err := NewTestContext(t)
	require.NoError(t, err)

	moduleGVK := gvk.AIHub
	moduleCRNN := types.NamespacedName{Name: modelRegistryModuleCRName}
	controllerNN := types.NamespacedName{
		Namespace: tc.AppsNamespace,
		Name:      modelRegistryModuleOperatorDeployment,
	}
	registriesNSNN := types.NamespacedName{Name: modelRegistryTestNamespace}

	var originalRegistriesNamespace string

	testCases := []TestCase{
		{"Validate component enabled", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Smoke, Tier1)

			// Capture the original registriesNamespace so cleanup can restore it.
			if dsc := tc.FetchDataScienceCluster(); dsc != nil {
				originalRegistriesNamespace = dsc.Spec.Components.ModelRegistry.RegistriesNamespace
			}

			// Patch DSC to Managed with a non-default registriesNamespace.
			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithMutateFunc(func(obj *unstructured.Unstructured) error {
					if err := unstructured.SetNestedField(obj.Object, "Managed", "spec", "components", "modelregistry", "managementState"); err != nil {
						return err
					}
					return unstructured.SetNestedField(obj.Object, modelRegistryTestNamespace, "spec", "components", "modelregistry", "registriesNamespace")
				}),
				WithCondition(And(
					jq.Match(`.spec.components.modelregistry.managementState == "Managed"`),
					jq.Match(`.spec.components.modelregistry.registriesNamespace == "%s"`, modelRegistryTestNamespace),
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

			// Assert DSC ModulesReady True.
			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeModulesReady, metav1.ConditionTrue)),
			)

			// Assert DSC ModelRegistryReady True + component managementState Managed.
			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
				WithCondition(And(
					jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, componentApi.ModelRegistryKind, metav1.ConditionTrue),
					jq.Match(`.status.components.modelregistry.managementState == "Managed"`),
				)),
				WithCustomErrorMsg("DataScienceCluster should have %sReady condition set to True with Managed state", componentApi.ModelRegistryKind),
			)
		}},
		{"Validate module CR spec", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Tier1)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.AIHub, moduleCRNN),
				WithCondition(And(
					jq.Match(`.spec.applicationNamespace == "%s"`, tc.AppsNamespace),
					jq.Match(`.spec.instancesNamespace == "%s"`, modelRegistryTestNamespace),
				)),
			)
		}},
		{"Validate registries namespace created", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Tier1)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.Namespace, registriesNSNN),
				WithCustomErrorMsg("non-default registries namespace %s should be auto-created", modelRegistryTestNamespace),
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
				WithCondition(jq.Match(`.status.components.modelregistry.releases | length > 0`)),
				WithCustomErrorMsg("DSC status.components.modelregistry.releases should be mirrored from module CR"),
			)
		}},
		{"Validate component disabled", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Smoke, Tier1)

			// Transition ModelRegistry to Removed and restore registriesNamespace in one patch.
			// The CEL immutability rule allows changing registriesNamespace only when
			// managementState is being set to Removed in the same mutation.
			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithMutateFunc(func(obj *unstructured.Unstructured) error {
					if err := unstructured.SetNestedField(obj.Object, "Removed", "spec", "components", "modelregistry", "managementState"); err != nil {
						return err
					}
					return unstructured.SetNestedField(obj.Object, originalRegistriesNamespace, "spec", "components", "modelregistry", "registriesNamespace")
				}),
				WithCondition(jq.Match(`.spec.components.modelregistry.managementState == "Removed"`)),
			)

			tc.EnsureResourceGone(WithMinimalObject(moduleGVK, moduleCRNN))
			tc.EnsureResourceGone(WithMinimalObject(gvk.Deployment, controllerNN))

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithCondition(And(
					jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, componentApi.ModelRegistryKind, metav1.ConditionFalse),
					jq.Match(`.status.conditions[] | select(.type == "%sReady") | .reason == "%s"`, componentApi.ModelRegistryKind, status.RemovedReason),
				)),
				WithCustomErrorMsg("DataScienceCluster should have %sReady condition set to False/Removed", componentApi.ModelRegistryKind),
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
