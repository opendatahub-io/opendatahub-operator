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

type TrainerTestCtx struct {
	*ComponentTestCtx
}

func trainerTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(t, &componentApi.Trainer{})
	require.NoError(t, err)

	componentCtx := TrainerTestCtx{
		ComponentTestCtx: ct,
	}

	// Define test cases.
	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate component releases", componentCtx.ValidateComponentReleases},
		{"Validate external operator degraded condition monitoring", componentCtx.ValidateExternalOperatorDegradedMonitoring},
		{"Validate resource deletion recovery", componentCtx.ValidateAllDeletionRecovery},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateExternalOperatorDegradedMonitoring ensures that when the external JobSet operator CR
// has degraded conditions, they properly propagate to the component CR and DSC CR,
// and that recovery is properly reflected as well.
//
// Validates the full condition propagation chain:
// External Operator CR > Trainer Component CR > DataScienceCluster CR.
func (tc *TrainerTestCtx) ValidateExternalOperatorDegradedMonitoring(t *testing.T) {
	t.Helper()

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
			name:            "JobSetOperatorStaticResourcesDegraded=True triggers component degradation",
			conditionType:   "JobSetOperatorStaticResourcesDegraded",
			conditionStatus: metav1.ConditionTrue,
		},
		{
			name:            "Available=False triggers component degradation",
			conditionType:   "Available",
			conditionStatus: metav1.ConditionFalse,
		},
	}

	trainerNN := types.NamespacedName{Name: componentApi.TrainerInstanceName}

	t.Log("Verifying Trainer component is healthy even without JobSetOperator CR.")
	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, trainerNN),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionDependenciesAvailable, metav1.ConditionTrue),
		),
	)

	t.Log("Creating JobSetOperator CR for condition propagation tests.")
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.JobSetOperatorV1, types.NamespacedName{Name: "cluster", Namespace: jobSetOpNamespace}),
		WithMutateFunc(func(obj *unstructured.Unstructured) error {
			return unstructured.SetNestedField(obj.Object, "Managed", "spec", "managementState")
		}),
		WithCustomErrorMsg("Failed to create JobSetOperator CR for degraded monitoring test"),
	)

	t.Log("Scenario 2: Verifying Trainer component is healthy with JobSetOperator CR present (no degraded conditions).")
	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, trainerNN),
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

	t.Log("Scaling down JobSet operator deployment to prevent condition reset.")
	originalReplicas := tc.scaleJobSetOperator(t, 0)

	// Run each test case (inject condition, verify, clear condition, verify recovery)
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			tc.runDegradedConditionTest(t, testCase)
		})
	}

	t.Log("Scaling JobSet operator deployment back up.")
	tc.scaleJobSetOperator(t, originalReplicas)

	t.Log("All external operator degraded condition monitoring tests passed")
}

// ensureJobSetBaseline clears JobSet conditions, asserts Trainer component/DSC health.
// Returns the JobSet CR for use in test assertions.
func (tc *TrainerTestCtx) ensureJobSetBaseline(t *testing.T) *unstructured.Unstructured {
	t.Helper()

	trainerNN := types.NamespacedName{Name: componentApi.TrainerInstanceName}
	jobSetCR := tc.FetchSingleResourceOfKind(gvk.JobSetOperatorV1, jobSetOpNamespace)

	tc.ClearAllConditionsFromResourceStatus(jobSetCR)
	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, trainerNN),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionDependenciesAvailable, metav1.ConditionTrue)),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue)),
	)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionTrue)),
	)

	return jobSetCR
}

// jobSetOperatorDeploymentName is the name of the JobSet operator deployment within
// the CSV. We use this when patching the CSV to scale the operator.
const jobSetOperatorDeploymentName = "jobset-operator"

// scaleJobSetOperator scales the JobSet operator deployment by patching the CSV.
// OLM will enforce the replica count, preventing manual overrides.
// Returns the original replica count before scaling.
func (tc *TrainerTestCtx) scaleJobSetOperator(t *testing.T, replicas int32) int32 {
	t.Helper()

	t.Logf("Scaling JobSet operator via CSV in namespace %s to %d replicas.", jobSetOpNamespace, replicas)
	originalReplicas := tc.ScaleCSVDeploymentReplicas(
		jobSetOpNamespace,
		"jobset",
		jobSetOperatorDeploymentName,
		replicas,
	)
	t.Logf("JobSet operator deployment scaled to %d replicas in namespace %s.", replicas, jobSetOpNamespace)
	return originalReplicas
}

// runDegradedConditionTest runs a single degraded condition test case.
// It injects a condition, verifies propagation, then recovers and verifies cleanup.
func (tc *TrainerTestCtx) runDegradedConditionTest(t *testing.T, testCase degradedConditionTestCase) {
	t.Helper()

	t.Logf("Running test case: %s (Condition: %s=%s)", testCase.name, testCase.conditionType, testCase.conditionStatus)

	trainerNN := types.NamespacedName{Name: componentApi.TrainerInstanceName}

	// Establish baseline (clears conditions, asserts healthy)
	jobSetCR := tc.ensureJobSetBaseline(t)

	t.Logf("Simulating external operator degradation: Injecting %s=%s into operator CR.", testCase.conditionType, testCase.conditionStatus)
	tc.InjectConditionIntoResourceStatus(
		jobSetCR,
		testCase.conditionType,
		testCase.conditionStatus,
		"TestInjected",
		"Simulated condition for e2e test: "+testCase.conditionType+"="+string(testCase.conditionStatus),
	)

	t.Logf("Verifying Trainer component CR (%s) reacts by setting DependenciesAvailable=False and Ready=False.", trainerNN)
	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, trainerNN),
		WithCondition(
			And(
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionDependenciesAvailable, metav1.ConditionFalse),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .reason == "%s"`, status.ConditionDependenciesAvailable, "DependencyDegraded"),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("Dependencies degraded")`, status.ConditionDependenciesAvailable),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .message | contains("%s")`, status.ConditionDependenciesAvailable, testCase.conditionType),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionFalse),
			),
		),
	)

	t.Logf("Verifying DSC CR (%s) reflects the component's degraded state (TrainerReady=False).", tc.DataScienceClusterNamespacedName)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionFalse),
		),
	)

	t.Logf("Clearing injected condition %s from operator CR to test recovery.", testCase.conditionType)
	jobSetCR = tc.FetchSingleResourceOfKind(gvk.JobSetOperatorV1, jobSetOpNamespace)
	tc.RemoveConditionFromResourceStatus(jobSetCR, testCase.conditionType)

	t.Logf("Verifying Trainer component CR (%s) recovers (DependenciesAvailable=True, Ready=True).", trainerNN)
	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, trainerNN),
		WithCondition(
			And(
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionDependenciesAvailable, metav1.ConditionTrue),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
			),
		),
	)

	t.Logf("Verifying DSC CR (%s) recovers (TrainerReady=True).", tc.DataScienceClusterNamespacedName)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionTrue),
		),
	)

	t.Logf("Test case passed: %s", testCase.name)
}
