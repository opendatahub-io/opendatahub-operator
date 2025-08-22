package e2e_test

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

type OperatorResilienceTestCtx struct {
	*TestContext
}

const expectedReplicas = 3 // Number of replicas for the deployment

func operatorResilienceTestSuite(t *testing.T) {
	t.Helper()

	// Initialize the test context.
	tc, err := NewTestContext(t)
	tc.g.Expect(err).ShouldNot(HaveOccurred(), "Failed to initialize test context")
	// Create an instance of resilience test context.
	resilienceTestCtx := OperatorResilienceTestCtx{
		TestContext: tc,
	}

	// Define resilience test cases.
	testCases := []TestCase{
		{name: "Validate operator deployment health", testFn: resilienceTestCtx.ValidateOperatorDeployment},
		{name: "Validate leader election behavior", testFn: resilienceTestCtx.ValidateLeaderElectionBehavior},
		{name: "Validate pod recovery resilience", testFn: resilienceTestCtx.ValidatePodRecoveryResilience},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateOperatorDeployment checks if the operator deployment exists and is correctly configured.
// This runs in resilience tests where platform detection works properly.
func (tc *OperatorResilienceTestCtx) ValidateOperatorDeployment(t *testing.T) {
	t.Helper()

	deploymentName := tc.getControllerDeploymentName()

	// Verify if the operator deployment is created and healthy
	tc.EnsureDeploymentReady(types.NamespacedName{
		Namespace: tc.OperatorNamespace,
		Name:      deploymentName,
	}, expectedReplicas)
}

// ValidateLeaderElectionBehavior validates that leader election works correctly when the current leader pod is deleted.
func (tc *OperatorResilienceTestCtx) ValidateLeaderElectionBehavior(t *testing.T) {
	t.Helper()

	// Find and delete current leader
	originalLeader := tc.findLeaderPodFromLeases()
	tc.g.Expect(originalLeader).ShouldNot(BeEmpty(), "Failed to find leader pod")

	tc.DeleteResource(
		WithMinimalObject(gvk.Pod, types.NamespacedName{
			Name:      originalLeader,
			Namespace: tc.OperatorNamespace,
		}),
		WithWaitForDeletion(true),
	)

	// Verify new leader elected
	tc.g.Eventually(func() string {
		return tc.findLeaderPodFromLeases()
	}).Should(And(
		Not(BeEmpty()),
		Not(Equal(originalLeader)),
	), "New leader should be elected")

	// Ensure system still works
	tc.validateSystemHealth(t)
}

// ValidatePodRecoveryResilience validates pod recovery after deletion.
func (tc *OperatorResilienceTestCtx) ValidatePodRecoveryResilience(t *testing.T) {
	t.Helper()

	selector := tc.getOperatorPodSelector()
	pods := tc.getOperatorPods(selector)
	tc.g.Expect(pods).ShouldNot(BeEmpty(), "No controller manager pods found")

	originalCount := len(pods)

	// Delete any pod
	tc.DeleteResource(
		WithMinimalObject(gvk.Pod, types.NamespacedName{
			Name:      pods[0].GetName(),
			Namespace: pods[0].GetNamespace(),
		}),
		WithWaitForDeletion(true),
	)

	// Wait for recovery
	tc.g.Eventually(func() int {
		return len(tc.getOperatorPods(selector))
	}).Should(BeNumerically(">=", originalCount), "Pods should recover")

	// Validate deployment and pod health
	tc.validateDeploymentHealth(t)
	tc.validatePodHealth(t, selector)
	tc.validateSystemHealth(t)
}

// findLeaderPodFromLeases finds current leader pod name from lease resources.
func (tc *OperatorResilienceTestCtx) findLeaderPodFromLeases() string {
	leases := tc.FetchResources(
		WithMinimalObject(gvk.Lease, types.NamespacedName{Namespace: tc.OperatorNamespace}),
		WithListOptions(&client.ListOptions{Namespace: tc.OperatorNamespace}),
	)

	for _, lease := range leases {
		if leaderPod := tc.extractLeaderFromLease(lease); leaderPod != "" {
			return leaderPod
		}
	}
	return ""
}

// extractLeaderFromLease extracts leader pod name from a lease object.
func (tc *OperatorResilienceTestCtx) extractLeaderFromLease(lease unstructured.Unstructured) string {
	holderIdentity, _, _ := unstructured.NestedString(lease.Object, "spec", "holderIdentity")
	controllerDeployment := tc.getControllerDeploymentName()
	if holderIdentity != "" && strings.Contains(holderIdentity, controllerDeployment) {
		// holderIdentity format is typically: "podname_uuid"
		// Extract the pod name (everything before the last underscore)
		lastUnderscoreIndex := strings.LastIndex(holderIdentity, "_")
		if lastUnderscoreIndex > 0 {
			podName := holderIdentity[:lastUnderscoreIndex]
			// Verify the pod name contains the deployment name
			if strings.Contains(podName, controllerDeployment) {
				return podName
			}
		}
	}
	return ""
}

// getOperatorPods returns current operator pods matching the selector.
func (tc *OperatorResilienceTestCtx) getOperatorPods(selector labels.Selector) []unstructured.Unstructured {
	return tc.FetchResources(
		WithMinimalObject(gvk.Pod, types.NamespacedName{Namespace: tc.OperatorNamespace}),
		WithListOptions(&client.ListOptions{
			Namespace:     tc.OperatorNamespace,
			LabelSelector: selector,
		}),
	)
}

// validateDeploymentHealth checks deployment readiness and replica counts.
func (tc *OperatorResilienceTestCtx) validateDeploymentHealth(t *testing.T) {
	t.Helper()

	controllerDeployment := tc.getControllerDeploymentName()
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Namespace: tc.OperatorNamespace,
			Name:      controllerDeployment,
		}),
		WithCondition(
			And(
				jq.Match(`.status.readyReplicas == .status.replicas`),
				jq.Match(`.status.availableReplicas == .status.replicas`),
			),
		),
		WithCustomErrorMsg("Deployment should be healthy with all replicas ready"),
	)
}

// validatePodHealth checks pod health including readiness and restart counts.
func (tc *OperatorResilienceTestCtx) validatePodHealth(t *testing.T, selector labels.Selector) {
	t.Helper()

	tc.g.Eventually(func() bool {
		pods := tc.getOperatorPods(selector)
		if len(pods) == 0 {
			return false
		}

		for _, pod := range pods {
			// Check that each pod is running
			phase, _, _ := unstructured.NestedString(pod.Object, "status", "phase")
			if phase != "Running" {
				return false
			}

			// Check pod readiness
			podConditions, found, _ := unstructured.NestedSlice(pod.Object, "status", "conditions")
			if !found {
				return false
			}

			podReady := false
			for _, condition := range podConditions {
				if conditionMap, ok := condition.(map[string]interface{}); ok {
					conditionType, _, _ := unstructured.NestedString(conditionMap, "type")
					conditionStatus, _, _ := unstructured.NestedString(conditionMap, "status")
					if conditionType == "Ready" && conditionStatus == "True" {
						podReady = true
						break
					}
				}
			}
			if !podReady {
				return false
			}

			// Check for restart counts indicating crashes
			containerStatuses, found, _ := unstructured.NestedSlice(pod.Object, "status", "containerStatuses")
			if found {
				for _, containerStatus := range containerStatuses {
					if statusMap, ok := containerStatus.(map[string]interface{}); ok {
						if restartCount, found := statusMap["restartCount"]; found {
							if count, ok := restartCount.(int64); ok && count > 0 {
								t.Logf("Warning: Pod %s has restart count: %d", pod.GetName(), count)
							}
						}
					}
				}
			}
		}
		return true
	}).Should(BeTrue(), "All operator pods should be running and ready without critical errors")
}

// validateSystemHealth ensures DSCI and DSC remain ready after operations.
func (tc *OperatorResilienceTestCtx) validateSystemHealth(t *testing.T) {
	t.Helper()

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithCondition(jq.Match(`.status.phase == "%s"`, status.ConditionTypeReady)),
		WithCustomErrorMsg("DSCI should remain Ready after pod operations"),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(`.status.phase == "%s"`, status.ConditionTypeReady)),
		WithCustomErrorMsg("DSC should remain Ready after pod operations"),
	)
}
