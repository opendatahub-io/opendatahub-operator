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
	aihubTestNamespace            = "e2e-aihub"
	aiHubReadyCondition           = "AIHubReady"
)

type aihubSuiteState struct {
	createdInstancesNamespace bool
}

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
	aihubInstancesNSNN := types.NamespacedName{Name: aihubTestNamespace}

	suiteState := registerAIHubSuiteCleanup(t, tc, aihubInstancesNSNN)

	testCases := []TestCase{
		{"Validate component enabled", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Smoke, Tier1)

			// Record whether the namespace predates the suite: this suite must
			// never delete a namespace it did not create on a shared cluster.
			// FetchResource yields nil for a namespace that does not exist.
			suiteState.createdInstancesNamespace = tc.FetchResource(
				WithMinimalObject(gvk.Namespace, aihubInstancesNSNN),
			) == nil

			// instancesNamespace is immutable while AI Hub is Managed; pass
			// through Removed so the next patch can set a new namespace.
			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithMutateFunc(testf.Transform(`.spec.components.aiHub.managementState = "Removed"`)),
				WithCondition(jq.Match(`.spec.components.aiHub.managementState == "Removed"`)),
			)

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
					jq.Match(`.spec.applicationNamespace == "%s"`, tc.AppsNamespace),
					jq.Match(`.spec.instancesNamespace == "%s"`, aihubTestNamespace),
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
			err := tc.Client().Get(t.Context(), aihubInstancesNSNN, namespace)
			if k8serr.IsNotFound(err) {
				t.Cleanup(func() {
					tc.DeleteResource(WithMinimalObject(gvk.Namespace, aihubInstancesNSNN), WithIgnoreNotFound(true), WithWaitForDeletion(true))
				})
			} else {
				require.NoError(t, err)
			}

			snapshotDSCFields(t, tc, gvk.DataScienceClusterV2, []string{"spec", "components", "modelregistry"})
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
					jq.Match(`.spec.applicationNamespace == "%s"`, tc.AppsNamespace),
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
		{"Validate AI Hub instances namespace created", func(t *testing.T) {
			t.Helper()
			skipUnless(t, Tier1)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.Namespace, aihubInstancesNSNN),
				WithCustomErrorMsg("non-default AI Hub instances namespace %s should be auto-created", aihubTestNamespace),
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

			// The suite-level cleanup restores instancesNamespace after moving AI
			// Hub through Removed; changing it here is not necessary.
			tc.EventuallyResourcePatched(
				WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
				WithMutateFunc(testf.Transform(`.spec.components.aiHub.managementState = "Removed"`)),
				WithCondition(jq.Match(`.spec.components.aiHub.managementState == "Removed"`)),
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

			// AI Hub creates this namespace without an owner reference and
			// intentionally leaves it after removal. Assert that contract before
			// suite cleanup deletes a namespace created by this run.
			tc.EnsureResourceExists(
				WithMinimalObject(gvk.Namespace, aihubInstancesNSNN),
				WithCustomErrorMsg("AI Hub instances namespace should remain after the component is removed"),
			)
		}},
	}

	RunTestCases(t, testCases)
}

func registerAIHubSuiteCleanup(t *testing.T, tc *TestContext, instancesNSNN types.NamespacedName) *aihubSuiteState {
	t.Helper()

	state := &aihubSuiteState{}

	// AI Hub creates instancesNamespace with CreateIfNotExists but deliberately
	// does not own it: removing AI Hub must not cascade-delete a namespace that
	// may contain user registries. Clean it up ourselves only when it did not
	// exist before this suite.
	t.Cleanup(func() {
		if !state.createdInstancesNamespace {
			return
		}

		tc.DeleteResource(
			WithMinimalObject(gvk.Namespace, instancesNSNN),
			WithIgnoreNotFound(true),
			WithWaitForDeletion(true),
		)
	})

	// Restore only the fields this suite mutates; component suites run
	// concurrently and share one DSC.
	snapshotDSCFields(t, tc, gvk.DataScienceCluster,
		[]string{"spec", "components", "aiHub", "managementState"},
		[]string{"spec", "components", "aiHub", "instancesNamespace"},
	)
	// instancesNamespace is immutable while Managed. This cleanup is registered
	// after snapshotDSCFields so it runs first (t.Cleanup is LIFO) and moves AI
	// Hub to Removed before the snapshot restoration changes the namespace.
	t.Cleanup(func() {
		tc.EventuallyResourcePatched(
			WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
			WithMutateFunc(testf.Transform(`.spec.components.aiHub.managementState = "Removed"`)),
			WithCondition(jq.Match(`.spec.components.aiHub.managementState == "Removed"`)),
		)
	})

	return state
}
