package e2e_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	azurev1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/azure/v1alpha1"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/kserve"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const (
	// lwsOperatorDeploymentName is the name of the LWS operator deployment within
	// the CSV. We use this when patching the CSV to scale the operator.
	lwsOperatorDeploymentName = "openshift-lws-operator"
)

type KserveTestCtx struct {
	*ComponentTestCtx
}

func kserveTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(t, &componentApi.Kserve{})
	require.NoError(t, err)

	componentCtx := KserveTestCtx{
		ComponentTestCtx: ct,
	}

	// Set per-operation timeout defaults for all operations in this suite.
	// KServe initialization involves complex CRD/feature gate setup that requires longer timeouts.
	componentCtx.DefaultResourceOpts = []ResourceOpts{
		WithEventuallyTimeout(ct.TestTimeouts.longEventuallyTimeout),
		WithEventuallyPollingInterval(ct.TestTimeouts.defaultEventuallyPollInterval),
	}

	// Define test cases.
	testCases := make([]TestCase, 0, 11)
	testCases = append(testCases,
		TestCase{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		TestCase{"Validate component spec", componentCtx.ValidateSpec},
		TestCase{"Validate model controller", componentCtx.ValidateModelControllerInstance},
		TestCase{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		TestCase{"Validate no Kserve FeatureTrackers", componentCtx.ValidateNoKserveFeatureTrackers},
		TestCase{"Validate VAP created when kserve is enabled", componentCtx.ValidateS3SecretCheckBucketExist},
		TestCase{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		TestCase{"Validate component releases", componentCtx.ValidateComponentReleases},
		TestCase{"Validate well-known LLMInferenceServiceConfig versioning", componentCtx.ValidateLLMInferenceServiceConfigVersioned},
	)

	// Always run deletion recovery and component disable tests last
	testCases = append(testCases,
		TestCase{"Validate resource deletion recovery", componentCtx.ValidateAllDeletionRecovery},
		TestCase{"Validate component disabled", componentCtx.ValidateComponentDisabled},
	)
	// Run the test suite.
	RunTestCases(t, testCases)
}

// kserveDegradedMonitoringTestSuite runs only the external operator degraded monitoring tests.
func kserveDegradedMonitoringTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(t, &componentApi.Kserve{})
	require.NoError(t, err)

	componentCtx := KserveTestCtx{
		ComponentTestCtx: ct,
	}

	// Set per-operation timeout defaults — KServe initialization is slow (CRD/feature gate setup).
	// Without this, operations fall back to the 5m baseline and flake in slower CI environments.
	componentCtx.DefaultResourceOpts = []ResourceOpts{
		WithEventuallyTimeout(ct.TestTimeouts.longEventuallyTimeout),
		WithEventuallyPollingInterval(ct.TestTimeouts.defaultEventuallyPollInterval),
	}

	testCases := []TestCase{
		// we must enable the component first since this suite runs isolated from other component tests
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate external operator degraded condition monitoring", componentCtx.ValidateExternalOperatorDegradedMonitoring},
	}
	RunTestCases(t, testCases)
}

// ValidateSpec ensures that the Kserve instance configuration matches the expected specification.
func (tc *KserveTestCtx) ValidateSpec(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke)

	tc.SkipIfXKSCluster(t)

	// Retrieve the DataScienceCluster instance.
	dsc := tc.FetchDataScienceCluster()

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Kserve, types.NamespacedName{Name: componentApi.KserveInstanceName}),
		WithCondition(And(
			// Validate management states of NIM and serving components.
			jq.Match(`.spec.nim.managementState == "%s"`, dsc.Spec.Components.Kserve.NIM.ManagementState),
		),
		),
	)
}

// ValidateNoKserveFeatureTrackers ensures there are no FeatureTrackers for Kserve.
func (tc *KserveTestCtx) ValidateNoKserveFeatureTrackers(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke)

	// FeatureTrackers are not supported on XKS platform (also CRD are not installed), skip the test
	tc.SkipIfXKSCluster(t)

	tc.EnsureResourcesDoNotExist(
		WithMinimalObject(gvk.FeatureTracker, tc.NamespacedName),
		WithListOptions(&client.ListOptions{
			Namespace: tc.AppsNamespace,
			LabelSelector: k8slabels.SelectorFromSet(
				k8slabels.Set{
					labels.PlatformPartOf: strings.ToLower(tc.GVK.Kind),
				},
			),
		}),
		WithCustomErrorMsg("Expected no KServe-related FeatureTracker resources to be present"),
	)
}

// ValidateLLMInferenceServiceConfigVersioned validates that well-known LLMInferenceServiceConfig
// resources (marked with serving.kserve.io/well-known-config annotation) in the system namespace
// have names prefixed with a semver version.
func (tc *KserveTestCtx) ValidateLLMInferenceServiceConfigVersioned(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)

	configGVKs := []schema.GroupVersionKind{
		gvk.LLMInferenceServiceConfigV1Alpha1,
		gvk.LLMInferenceServiceConfigV1Alpha2,
	}

	for _, configGVK := range configGVKs {
		t.Run(configGVK.Version, func(t *testing.T) {
			// Validate that all well-known LLMInferenceServiceConfig resources have versioned names
			// Expected format: vX-Y-Z-<config-name> where X, Y, Z are numbers
			// Only check resources with the well-known-config annotation set to true
			tc.EnsureResourcesExist(
				WithMinimalObject(configGVK, types.NamespacedName{Namespace: tc.AppsNamespace}),
				WithListOptions(&client.ListOptions{
					Namespace: tc.AppsNamespace,
				}),
				WithCondition(jq.Match(`
					map(select(.metadata.annotations["%s"] == "%s"))
					| length > 0
				`,
					kserve.LLMInferenceServiceConfigWellKnownAnnotationKey,
					kserve.LLMInferenceServiceConfigWellKnownAnnotationValue,
				)),
				WithCustomErrorMsg("Expected at least one well-known LLMInferenceServiceConfig %s to exist", configGVK.Version),
			)

			// Validate that all well-known configs follow the versioned naming pattern
			tc.EnsureResourcesExist(
				WithMinimalObject(configGVK, types.NamespacedName{Namespace: tc.AppsNamespace}),
				WithListOptions(&client.ListOptions{
					Namespace: tc.AppsNamespace,
				}),
				WithCondition(jq.Match(`
					map(select(.metadata.annotations["%s"] == "%s"))
					| all(.metadata.name | test("^v[0-9]+-[0-9]+-[0-9]+-.*"))
				`,
					kserve.LLMInferenceServiceConfigWellKnownAnnotationKey,
					kserve.LLMInferenceServiceConfigWellKnownAnnotationValue,
				)),
				WithCustomErrorMsg("All well-known LLMInferenceServiceConfig %s resources should have names starting with a semver version (vX-Y-Z-)", configGVK.Version),
			)
		})
	}
}

// ensureLWSBaseline clears LWS conditions, asserts Kserve component and DSC health.
// Returns the LWS CR for use in test assertions.
func (tc *KserveTestCtx) ensureLWSBaseline(t *testing.T) *unstructured.Unstructured {
	t.Helper()

	kserveNN := types.NamespacedName{Name: componentApi.KserveInstanceName}
	lwsCR := tc.FetchSingleResourceOfKind(gvk.LeaderWorkerSetOperatorV1, leaderWorkerSetNamespace)

	tc.ClearAllConditionsFromResourceStatus(lwsCR)
	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, kserveNN),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionDependenciesAvailable, metav1.ConditionTrue)),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue)),
	)

	if !tc.IsXKS() {
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
			WithCondition(jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionTrue)),
		)
	}

	return lwsCR
}

// ValidateExternalOperatorDegradedMonitoring ensures that when the external LeaderWorkerSet operator CR
// has degraded conditions, they properly propagate to the component CR and DSC CR,
// and that recovery is properly reflected as well.
//
// Validates the full condition propagation chain:
// External Operator CR > Kserve Component CR > DataScienceCluster CR.
func (tc *KserveTestCtx) ValidateExternalOperatorDegradedMonitoring(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)

	kserveNN := types.NamespacedName{Name: componentApi.KserveInstanceName}

	if tc.IsXKS() {
		tc.runXKSDegradedMonitoringTest(t, kserveNN)
		return
	}

	// condition types monitored by the Kserve Component
	testCases := []degradedConditionTestCase{
		{
			name:            "Degraded=True triggers component degradation",
			conditionType:   "Degraded",
			conditionStatus: metav1.ConditionTrue,
		},
		{
			name:            "TargetConfigControllerDegraded=True triggers component degradation",
			conditionType:   "TargetConfigControllerDegraded",
			conditionStatus: metav1.ConditionTrue,
		},
		{
			name:            "Available=False triggers component degradation",
			conditionType:   "Available",
			conditionStatus: metav1.ConditionFalse,
		},
	}

	// Scale external operator only; per-case helpers handle condition clears
	t.Logf("Ensuring LWS operator deployment is scaled down to prevent condition reset (namespace=%s, name=%s).", leaderWorkerSetNamespace, lwsOperatorDeploymentName)
	originalReplicas := tc.scaleLWSOperator(t, 0)

	t.Logf("Verifying Kserve component is healthy without LeaderWorkerSetOperator CR (namespace=%s, name=%s).", tc.AppsNamespace, kserveNN.Name)
	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, kserveNN),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionDependenciesAvailable, metav1.ConditionTrue),
		),
	)

	t.Logf("Creating LeaderWorkerSetOperator CR for condition propagation tests (namespace=%s, name=%s).", leaderWorkerSetNamespace, "cluster")
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.LeaderWorkerSetOperatorV1, types.NamespacedName{Name: "cluster", Namespace: leaderWorkerSetNamespace}),
		WithMutateFunc(func(obj *unstructured.Unstructured) error {
			return unstructured.SetNestedField(obj.Object, "Managed", "spec", "managementState")
		}),
		WithCustomErrorMsg("Failed to create LeaderWorkerSetOperator CR for degraded monitoring test"),
	)

	t.Logf("Verifying Kserve component is healthy with LeaderWorkerSetOperator CR present (namespace=%s, name=%s).", tc.AppsNamespace, kserveNN.Name)
	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, kserveNN),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionDependenciesAvailable, metav1.ConditionTrue),
		),
	)

	t.Logf("Verifying DSC is healthy before tests (namespace=%s, name=%s).", tc.DataScienceClusterNamespacedName.Namespace, tc.DataScienceClusterNamespacedName.Name)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionTrue),
		),
	)

	// Run each test case (inject condition, verify, clear condition, verify recovery)
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			tc.runDegradedConditionTest(t, testCase)
		})
	}

	t.Logf("Scaling LWS operator deployment back up (namespace=%s, name=%s).", leaderWorkerSetNamespace, lwsOperatorDeploymentName)
	tc.scaleLWSOperator(t, originalReplicas)

	t.Log("All external operator degraded condition monitoring tests passed")
}

// scaleLWSOperator scales the LWS operator deployment by patching the CSV.
// blocks until the deployment reaches the target replica count.
func (tc *KserveTestCtx) scaleLWSOperator(t *testing.T, replicas int32) int32 {
	t.Helper()

	t.Logf("Scaling LWS operator via CSV in namespace %s to %d replicas.", leaderWorkerSetNamespace, replicas)
	originalReplicas := tc.ScaleCSVDeploymentReplicas(
		leaderWorkerSetNamespace,
		"leader-worker-set",
		lwsOperatorDeploymentName,
		replicas,
	)
	t.Logf("LWS operator deployment scaled to %d replicas in namespace %s.", replicas, leaderWorkerSetNamespace)
	return originalReplicas
}

// runDegradedConditionTest runs a single degraded condition test case.
// It injects a condition, verifies propagation, then recovers and verifies cleanup.
func (tc *KserveTestCtx) runDegradedConditionTest(t *testing.T, testCase degradedConditionTestCase) {
	t.Helper()

	t.Logf("Running test case: %s (Condition: %s=%s)", testCase.name, testCase.conditionType, testCase.conditionStatus)

	kserveNN := types.NamespacedName{Name: componentApi.KserveInstanceName}

	// Establish baseline (clears conditions, asserts healthy)
	lwsCR := tc.ensureLWSBaseline(t)

	t.Logf("Simulating external operator degradation: Injecting %s=%s into operator CR.", testCase.conditionType, testCase.conditionStatus)
	tc.InjectConditionIntoResourceStatus(
		lwsCR,
		testCase.conditionType,
		testCase.conditionStatus,
		"TestInjected",
		"Simulated condition for e2e test: "+testCase.conditionType+"="+string(testCase.conditionStatus),
	)

	t.Logf("Verifying Kserve component CR (%s) records dependency degradation (info severity).", kserveNN)
	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, kserveNN),
		WithCondition(
			And(
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionDependenciesAvailable, metav1.ConditionFalse),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, status.ConditionDependenciesAvailable, "DependencyDegraded"),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("Dependencies degraded")`, status.ConditionDependenciesAvailable),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("%s")`, status.ConditionDependenciesAvailable, testCase.conditionType),
				// Informational dependency: Ready should remain True
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
			),
		),
	)

	t.Logf("Verifying DSC CR (%s) still shows KserveReady=True (info-level dependency doesn't flip Ready).", tc.DataScienceClusterNamespacedName)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(
			// KserveReady should stay True for info-level dependency
			jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionTrue),
		),
	)

	t.Logf("Clearing injected condition %s from operator CR to test recovery.", testCase.conditionType)
	lwsCR = tc.FetchSingleResourceOfKind(gvk.LeaderWorkerSetOperatorV1, leaderWorkerSetNamespace)
	tc.RemoveConditionFromResourceStatus(lwsCR, testCase.conditionType)

	t.Logf("Verifying Kserve component CR (%s) recovers (DependenciesAvailable=True, Ready=True).", kserveNN)
	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, kserveNN),
		WithCondition(
			And(
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionDependenciesAvailable, metav1.ConditionTrue),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
			),
		),
	)

	t.Logf("Verifying DSC CR (%s) recovers (KserveReady=True).", tc.DataScienceClusterNamespacedName)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionTrue),
		),
	)

	t.Logf("Test case passed: %s", testCase.name)
}

// runXKSDegradedMonitoringTest verifies that setting the AzureKubernetesEngine LWS dependency
// to Unmanaged causes the Kserve DependenciesAvailable condition to become False,
// and that setting it back to Managed restores it to True.
func (tc *KserveTestCtx) runXKSDegradedMonitoringTest(t *testing.T, kserveNN types.NamespacedName) {
	t.Helper()

	t.Logf("Verifying Kserve component DependenciesAvailable=True before test (name=%s).", kserveNN.Name)
	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, kserveNN),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionDependenciesAvailable, metav1.ConditionTrue),
		),
	)

	t.Logf("Setting AzureKubernetesEngine LWS managementPolicy to Unmanaged.")
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.AzureKubernetesEngine, types.NamespacedName{Name: azurev1alpha1.AzureKubernetesEngineInstanceName}),
		WithMutateFunc(func(obj *unstructured.Unstructured) error {
			return unstructured.SetNestedField(obj.Object, "Unmanaged", "spec", "dependencies", "lws", "managementPolicy")
		}),
		WithCustomErrorMsg("Failed to set AzureKubernetesEngine LWS managementPolicy to Unmanaged"),
	)

	t.Logf("Verifying Kserve component DependenciesAvailable=False after setting LWS to Unmanaged (name=%s).", kserveNN.Name)
	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, kserveNN),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionDependenciesAvailable, metav1.ConditionFalse),
		),
	)

	t.Logf("Setting AzureKubernetesEngine LWS managementPolicy back to Managed.")
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.AzureKubernetesEngine, types.NamespacedName{Name: azurev1alpha1.AzureKubernetesEngineInstanceName}),
		WithMutateFunc(func(obj *unstructured.Unstructured) error {
			return unstructured.SetNestedField(obj.Object, "Managed", "spec", "dependencies", "lws", "managementPolicy")
		}),
		WithCustomErrorMsg("Failed to set AzureKubernetesEngine LWS managementPolicy to Managed"),
	)

	t.Logf("Verifying Kserve component DependenciesAvailable=True after restoring LWS to Managed (name=%s).", kserveNN.Name)
	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, kserveNN),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionDependenciesAvailable, metav1.ConditionTrue),
		),
	)

	t.Log("XKS external operator degraded monitoring test passed")
}

// kserveModelCacheTestSuite validates ModelCache programmatic resource management.
// Runs in its own scenario group to avoid PSA label interference with other KServe tests.
func kserveModelCacheTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(t, &componentApi.Kserve{})
	require.NoError(t, err)

	componentCtx := KserveTestCtx{
		ComponentTestCtx: ct,
	}

	reset := componentCtx.OverrideEventuallyTimeout(ct.TestTimeouts.longEventuallyTimeout, ct.TestTimeouts.defaultEventuallyPollInterval)
	defer reset()

	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate ModelCache enabled", componentCtx.ValidateModelCacheEnabled},
		{"Validate ModelCache ConfigMap", componentCtx.ValidateModelCacheConfigMap},
		{"Validate ModelCache image reconcile", componentCtx.ValidateModelCacheImageReconcile},
		{"Validate ModelCache deletion recovery", componentCtx.ValidateModelCacheDeletionRecovery},
		{"Validate ModelCache disabled", componentCtx.ValidateModelCacheDisabled},
	}

	RunTestCases(t, testCases)
}

// fetchWorkerNodeName returns the name of a worker node in the cluster.
func (tc *KserveTestCtx) fetchWorkerNodeName(t *testing.T) string {
	t.Helper()

	nodes := tc.FetchResources(
		WithMinimalObject(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Node"}, types.NamespacedName{}),
		WithListOptions(&client.ListOptions{
			LabelSelector: k8slabels.SelectorFromSet(k8slabels.Set{
				"node-role.kubernetes.io/worker": "",
			}),
		}),
	)

	require.NotEmpty(t, nodes, "Expected at least one worker node in the cluster")
	return nodes[0].GetName()
}

// enableModelCache patches the DSC to enable ModelCache with the given node name and cache size.
func (tc *KserveTestCtx) enableModelCache(t *testing.T, nodeName string) {
	t.Helper()

	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(func(obj *unstructured.Unstructured) error {
			return unstructured.SetNestedField(obj.Object, map[string]interface{}{
				"managementState": "Managed",
				"cacheSize":       "5Gi",
				"nodeNames":       []interface{}{nodeName},
			}, "spec", "components", "kserve", "modelCache")
		}),
		WithCondition(
			jq.Match(`.spec.components.kserve.modelCache.managementState == "Managed"`),
		),
	)

	// Wait for KServe to reconcile successfully
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Kserve, types.NamespacedName{Name: componentApi.KserveInstanceName}),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
		),
	)
}

// ValidateModelCacheEnabled enables ModelCache and verifies all programmatic resources are created.
func (tc *KserveTestCtx) ValidateModelCacheEnabled(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)

	nodeName := tc.fetchWorkerNodeName(t)
	t.Logf("Using worker node %q for ModelCache tests", nodeName)

	tc.enableModelCache(t, nodeName)

	// Verify PV exists
	t.Log("Verifying PersistentVolume kserve-localmodelnode-pv exists")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.PersistentVolume, types.NamespacedName{Name: "kserve-localmodelnode-pv"}),
	)

	// Verify PVC exists in apps namespace
	t.Log("Verifying PersistentVolumeClaim kserve-localmodelnode-pvc exists")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.PersistentVolumeClaim, types.NamespacedName{
			Name:      "kserve-localmodelnode-pvc",
			Namespace: tc.AppsNamespace,
		}),
	)

	// Verify LocalModelNodeGroup exists
	t.Log("Verifying LocalModelNodeGroup 'workers' exists")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.LocalModelNodeGroup, types.NamespacedName{Name: "workers"}),
	)

	// Verify node is labeled
	t.Logf("Verifying node %q has label kserve/localmodel=worker", nodeName)
	tc.EnsureResourceExists(
		WithMinimalObject(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Node"}, types.NamespacedName{Name: nodeName}),
		WithCondition(
			jq.Match(`.metadata.labels["kserve/localmodel"] == "worker"`),
		),
	)

	// Verify namespace PSA label is privileged
	t.Logf("Verifying namespace %q has PSA enforce=privileged", tc.AppsNamespace)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: tc.AppsNamespace}),
		WithCondition(
			jq.Match(`.metadata.labels["%s"] == "privileged"`, labels.SecurityEnforce),
		),
	)
}

// ValidateModelCacheConfigMap verifies the inferenceservice-config ConfigMap is correctly
// configured for ModelCache: localModel.enabled=true and jobNamespace matches the apps namespace.
func (tc *KserveTestCtx) ValidateModelCacheConfigMap(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)

	t.Logf("Verifying inferenceservice-config ConfigMap has localModel.enabled=true and jobNamespace=%s", tc.AppsNamespace)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ConfigMap, types.NamespacedName{
			Name:      "inferenceservice-config",
			Namespace: tc.AppsNamespace,
		}),
		WithCondition(
			And(
				jq.Match(`.data.localModel | fromjson | .enabled == true`),
				jq.Match(`.data.localModel | fromjson | .jobNamespace == "%s"`, tc.AppsNamespace),
			),
		),
	)
}

// ValidateModelCacheImageReconcile verifies that the operator force-reconciles the
// modelcachePermissionFixImage in the inferenceservice-config ConfigMap when tampered.
// Skips if RELATED_IMAGE_ODH_KSERVE_AGENT_IMAGE is not set (the production code no-ops).
func (tc *KserveTestCtx) ValidateModelCacheImageReconcile(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)

	expectedImage := os.Getenv("RELATED_IMAGE_ODH_KSERVE_AGENT_IMAGE")
	if expectedImage == "" {
		t.Skip("RELATED_IMAGE_ODH_KSERVE_AGENT_IMAGE not set, skipping image reconcile test")
	}

	configMapNN := types.NamespacedName{
		Name:      "inferenceservice-config",
		Namespace: tc.AppsNamespace,
	}

	t.Log("Tampering modelcachePermissionFixImage in inferenceservice-config ConfigMap")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.ConfigMap, configMapNN),
		WithMutateFunc(func(obj *unstructured.Unstructured) error {
			data, _, _ := unstructured.NestedStringMap(obj.Object, "data")
			if data == nil {
				return errors.New("ConfigMap data is nil")
			}

			var openshiftConfig map[string]interface{}
			if err := json.Unmarshal([]byte(data[kserve.OpenshiftConfigKeyName]), &openshiftConfig); err != nil {
				return fmt.Errorf("failed to parse openshiftConfig: %w", err)
			}

			openshiftConfig["modelcachePermissionFixImage"] = "tampered-image:latest"
			updated, err := json.Marshal(openshiftConfig)
			if err != nil {
				return fmt.Errorf("failed to marshal openshiftConfig: %w", err)
			}

			return unstructured.SetNestedField(obj.Object, string(updated), "data", kserve.OpenshiftConfigKeyName)
		}),
		WithCondition(
			jq.Match(`.data.%s | fromjson | .modelcachePermissionFixImage == "tampered-image:latest"`, kserve.OpenshiftConfigKeyName),
		),
	)

	t.Logf("Verifying operator reconciles modelcachePermissionFixImage back to %q", expectedImage)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ConfigMap, configMapNN),
		WithCondition(
			jq.Match(`.data.%s | fromjson | .modelcachePermissionFixImage == "%s"`, kserve.OpenshiftConfigKeyName, expectedImage),
		),
	)
}

// ValidateModelCacheDeletionRecovery verifies the operator recreates the
// LocalModelNodeGroup after deletion.
// PV and PVC deletion recovery is not tested because the modelcache DaemonSet
// mounts the PVC, so the kubernetes.io/pvc-protection finalizer blocks PVC
// deletion while the DaemonSet pods are running.
func (tc *KserveTestCtx) ValidateModelCacheDeletionRecovery(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)

	t.Log("Deleting LocalModelNodeGroup 'workers' and verifying recreation")
	tc.EnsureResourceDeletedThenRecreated(
		WithMinimalObject(gvk.LocalModelNodeGroup, types.NamespacedName{Name: "workers"}),
	)
}

// ValidateModelCacheDisabled disables ModelCache and verifies cleanup:
// PSA label reverts to baseline, ConfigMap localModel.enabled=false.
func (tc *KserveTestCtx) ValidateModelCacheDisabled(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)

	t.Log("Disabling ModelCache by setting managementState=Removed")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.kserve.modelCache.managementState = "Removed"`)),
		WithCondition(
			jq.Match(`.spec.components.kserve.modelCache.managementState == "Removed"`),
		),
	)

	// Wait for KServe to reconcile successfully
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Kserve, types.NamespacedName{Name: componentApi.KserveInstanceName}),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
		),
	)

	// Verify namespace PSA label reverted to baseline
	t.Logf("Verifying namespace %q has PSA enforce=baseline", tc.AppsNamespace)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: tc.AppsNamespace}),
		WithCondition(
			jq.Match(`.metadata.labels["%s"] == "baseline"`, labels.SecurityEnforce),
		),
	)

	// Verify ConfigMap localModel.enabled is false
	t.Log("Verifying inferenceservice-config ConfigMap has localModel.enabled=false")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ConfigMap, types.NamespacedName{
			Name:      "inferenceservice-config",
			Namespace: tc.AppsNamespace,
		}),
		WithCondition(
			jq.Match(`.data.localModel | fromjson | .enabled == false`),
		),
	)
}
