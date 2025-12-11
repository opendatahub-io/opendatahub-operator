package e2e_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/kserve"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
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

	// Increase the global eventually timeout
	reset := componentCtx.OverrideEventuallyTimeout(ct.TestTimeouts.longEventuallyTimeout, ct.TestTimeouts.defaultEventuallyPollInterval)
	defer reset() // Make sure it's reset after all tests run

	// Define test cases.
	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate component spec", componentCtx.ValidateSpec},
		{"Validate model controller", componentCtx.ValidateModelControllerInstance},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate no Kserve FeatureTrackers", componentCtx.ValidateNoKserveFeatureTrackers},
		{"Validate VAP created when kserve is enabled", componentCtx.ValidateS3SecretCheckBucketExist},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate component releases", componentCtx.ValidateComponentReleases},
		{"Validate well-known LLMInferenceServiceConfig versioning", componentCtx.ValidateLLMInferenceServiceConfigVersioned},
		{"Validate external operator degraded condition monitoring", componentCtx.ValidateExternalOperatorDegradedMonitoring},
	}

	// Add webhook tests if enabled
	if testOpts.webhookTest {
		testCases = append(testCases,
			TestCase{"Validate connection webhook injection", componentCtx.ValidateConnectionWebhookInjection},
		)
	}

	// Always run deletion recovery and component disable tests last
	testCases = append(testCases,
		TestCase{"Validate resource deletion recovery", componentCtx.ValidateAllDeletionRecovery},
		TestCase{"Validate component disabled", componentCtx.ValidateComponentDisabled},
	)
	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateSpec ensures that the Kserve instance configuration matches the expected specification.
func (tc *KserveTestCtx) ValidateSpec(t *testing.T) {
	t.Helper()

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

// ValidateConnectionWebhookInjection validates that the connection webhook properly injects
// secrets into InferenceService resources with existing imagePullSecrets.
func (tc *KserveTestCtx) ValidateConnectionWebhookInjection(t *testing.T) {
	t.Helper()

	// Ensure KServe is in Managed state to enable webhook functionality
	tc.ValidateComponentEnabled(t)

	testNamespace := "glue-namespace"
	secretName := "glue-secret"
	isvcName := "glue-isvc"

	// Create test namespace
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: testNamespace}),
		WithCustomErrorMsg("Failed to create webhook test namespace"),
	)

	// Create a connection secret with OCI type
	tc.createConnectionSecret(secretName, testNamespace)

	// Create InferenceService with connection annotation and existing imagePullSecrets
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.InferenceServices, types.NamespacedName{Name: isvcName, Namespace: testNamespace}),
		WithMutateFunc(testf.TransformPipeline(
			// Set connection annotation
			testf.Transform(`.metadata.annotations."%s" = "%s"`, annotations.Connection, secretName),
			// Set predictor spec with model and existing imagePullSecrets
			testf.Transform(`.spec.predictor = {
				"model": {},
				"imagePullSecrets": [{"name": "existing-secret"}]
			}`),
		)),
		WithCustomErrorMsg("Failed to create InferenceService with webhook injection"),
	)

	// Validate that both the existing-secret and the new connection secret are present
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.InferenceServices, types.NamespacedName{Name: isvcName, Namespace: testNamespace}),
		WithCondition(jq.Match(`
			.spec.predictor.imagePullSecrets | length == 2
			and (map(.name) | contains(["existing-secret"]))
			and (map(.name) | contains(["%s"]))`,
			secretName)),
		WithCustomErrorMsg("InferenceService should have both existing and injected imagePullSecrets"),
	)

	// Cleanup the created test namespace
	tc.DeleteResource(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: testNamespace}),
		WithWaitForDeletion(true),
	)
}

// createConnectionSecret creates a connection secret with OCI type to test webhook.
func (tc *KserveTestCtx) createConnectionSecret(secretName, namespace string) {
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.Secret, types.NamespacedName{Name: secretName, Namespace: namespace}),
		WithMutateFunc(testf.TransformPipeline(
			// Set connection type annotation
			testf.Transform(`.metadata.annotations."%s" = "%s"`, annotations.ConnectionTypeProtocol, "oci"),
			// Set secret type
			testf.Transform(`.type = "%s"`, string(corev1.SecretTypeOpaque)),
			// Set secret data
			testf.Transform(`.data = {"credential": "mysecretjson"}`),
		)),
		WithCustomErrorMsg("Failed to create connection secret"),
	)
}

// ValidateLLMInferenceServiceConfigVersioned validates that well-known LLMInferenceServiceConfig
// resources (marked with serving.kserve.io/well-known-config annotation) in the system namespace
// have names prefixed with a semver version.
func (tc *KserveTestCtx) ValidateLLMInferenceServiceConfigVersioned(t *testing.T) {
	t.Helper()

	// Validate that all well-known LLMInferenceServiceConfig resources have versioned names
	// Expected format: vX-Y-Z-<config-name> where X, Y, Z are numbers
	// Only check resources with the well-known-config annotation set to true
	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.LLMInferenceServiceConfigV1Alpha1, types.NamespacedName{Namespace: tc.AppsNamespace}),
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
		WithCustomErrorMsg("Expected at least one well-known LLMInferenceServiceConfig to exist"),
	)

	// Validate that all well-known configs follow the versioned naming pattern
	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.LLMInferenceServiceConfigV1Alpha1, types.NamespacedName{Namespace: tc.AppsNamespace}),
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
		WithCustomErrorMsg("All well-known LLMInferenceServiceConfig resources should have names starting with a semver version (vX-Y-Z-)"),
	)
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
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionTrue)),
	)

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

	kserveNN := types.NamespacedName{Name: componentApi.KserveInstanceName}

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
