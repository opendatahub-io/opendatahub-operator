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

type OperatorTestCtx struct {
	*TestContext
}

const (
	controllerDeployment = "opendatahub-operator-controller-manager" // Name of the ODH deployment
	expectedReplicas     = 3                                         // Number of replicas for the deployment
)

func odhOperatorTestSuite(t *testing.T) {
	t.Helper()

	// Initialize the test context.
	tc, err := NewTestContext(t)
	tc.g.Expect(err).ShouldNot(HaveOccurred(), "Failed to initialize test context")

	// Ensure Service Mesh operator is installed before creating DSCI/DSC
	tc.EnsureOperatorInstalled(types.NamespacedName{
		Name:      serviceMeshOpName,
		Namespace: openshiftOperatorsNamespace,
	}, true)

	// Create an instance of test context.
	operatorTestCtx := OperatorTestCtx{
		TestContext: tc,
	}

	// Define test cases.
	testCases := []TestCase{
		{name: "Validate RHOAI Operator pod", testFn: operatorTestCtx.testODHDeployment},
		{name: "Validate CRDs owned by the operator", testFn: operatorTestCtx.ValidateOwnedCRDs},
		{name: "Validate pod recovery resilience", testFn: operatorTestCtx.ValidatePodRecoveryResilience},
		{name: "Validate leader election behavior", testFn: operatorTestCtx.ValidateLeaderElectionBehavior},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// testODHDeployment checks if the ODH deployment exists and is correctly configured.
func (tc *OperatorTestCtx) testODHDeployment(t *testing.T) {
	t.Helper()

	// Verify if the operator deployment is created
	controllerDeployment := "rhods-operator"
	tc.EnsureDeploymentReady(types.NamespacedName{Namespace: tc.OperatorNamespace, Name: controllerDeployment}, expectedReplicas)
}

// ValidateOwnedCRDs validates if the owned CRDs are properly created and available.
func (tc *OperatorTestCtx) ValidateOwnedCRDs(t *testing.T) {
	t.Helper()

	crdsTestCases := []struct {
		name string
		crd  string
	}{
		{"Datascience Cluster CRD", "datascienceclusters.datasciencecluster.opendatahub.io"},
		{"DataScienceCluster Initialization CRD", "dscinitializations.dscinitialization.opendatahub.io"},
		{"FeatureTracker CRD", "featuretrackers.features.opendatahub.io"},
		{"Dashboard CRD", "dashboards.components.platform.opendatahub.io"},
		{"Ray CRD", "rays.components.platform.opendatahub.io"},
		{"ModelRegistry CRD", "modelregistries.components.platform.opendatahub.io"},
		{"TrustyAI CRD", "trustyais.components.platform.opendatahub.io"},
		{"Kueue CRD", "kueues.components.platform.opendatahub.io"},
		{"TrainingOperator CRD", "trainingoperators.components.platform.opendatahub.io"},
		{"FeastOperator CRD", "feastoperators.components.platform.opendatahub.io"},
		{"DataSciencePipelines CRD", "datasciencepipelines.components.platform.opendatahub.io"},
		{"Workbenches CRD", "workbenches.components.platform.opendatahub.io"},
		{"Kserve CRD", "kserves.components.platform.opendatahub.io"},
		{"ModelMeshServing CRD", "modelmeshservings.components.platform.opendatahub.io"},
		{"ModelController CRD", "modelcontrollers.components.platform.opendatahub.io"},
		{"Monitoring CRD", "monitorings.services.platform.opendatahub.io"},
		{"LlamaStackOperator CRD", "llamastackoperators.components.platform.opendatahub.io"},
		{"CodeFlare CRD", "codeflares.components.platform.opendatahub.io"},
		{"Auth CRD", "auths.services.platform.opendatahub.io"},
	}

	for _, testCase := range crdsTestCases {
		t.Run("Validate "+testCase.name, func(t *testing.T) {
			t.Parallel()
			tc.EnsureCRDEstablished(testCase.crd)
		})
	}
}

// ValidateLeaderElectionBehavior validates that leader election works correctly
// when the current leader pod is deleted.
func (tc *OperatorTestCtx) ValidateLeaderElectionBehavior(t *testing.T) {
	t.Helper()

	tc.setupTestResources(t)
	t.Cleanup(func() { tc.cleanupTestResources() })

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
func (tc *OperatorTestCtx) ValidatePodRecoveryResilience(t *testing.T) {
	t.Helper()

	tc.setupTestResources(t)
	t.Cleanup(func() { tc.cleanupTestResources() })

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
	tc.validateDeploymentHealth(t, selector)
	tc.validatePodHealth(t, selector)
	tc.validateSystemHealth(t)
}

// setupTestResources creates DSCI and DSC resources.
func (tc *OperatorTestCtx) setupTestResources(t *testing.T) {
	t.Helper()

	dsci := tc.FetchResource(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
	)
	if dsci == nil {
		tc.EventuallyResourceCreatedOrUpdated(
			WithObjectToCreate(CreateDSCI(tc.DSCInitializationNamespacedName.Name, tc.AppsNamespace)),
			WithCondition(jq.Match(`.status.phase == "%s"`, status.ConditionTypeReady)),
			WithCustomErrorMsg("Failed to create DSCInitialization resource %s", tc.DSCInitializationNamespacedName.Name),
		)
	}

	dsc := tc.FetchResource(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
	)
	if dsc == nil {
		tc.EventuallyResourceCreatedOrUpdated(
			WithObjectToCreate(CreateDSC(tc.DataScienceClusterNamespacedName.Name)),
			WithCondition(jq.Match(`.status.phase == "%s"`, status.ConditionTypeReady)),
			WithCustomErrorMsg("Failed to create DataScienceCluster resource %s", tc.DataScienceClusterNamespacedName.Name),
		)
	}
}

// cleanupTestResources removes test resources.
func (tc *OperatorTestCtx) cleanupTestResources() {
	tc.DeleteResource(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithWaitForDeletion(true),
	)

	tc.DeleteResource(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithWaitForDeletion(true),
	)
}

// findLeaderPodFromLeases finds current leader pod name from lease resources.
func (tc *OperatorTestCtx) findLeaderPodFromLeases() string {
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
func (tc *OperatorTestCtx) extractLeaderFromLease(lease unstructured.Unstructured) string {
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
func (tc *OperatorTestCtx) getOperatorPods(selector labels.Selector) []unstructured.Unstructured {
	return tc.FetchResources(
		WithMinimalObject(gvk.Pod, types.NamespacedName{Namespace: tc.OperatorNamespace}),
		WithListOptions(&client.ListOptions{
			Namespace:     tc.OperatorNamespace,
			LabelSelector: selector,
		}),
	)
}

// validateDeploymentHealth checks deployment readiness and replica counts.
func (tc *OperatorTestCtx) validateDeploymentHealth(t *testing.T, selector labels.Selector) {
	t.Helper()

	controllerDeployment := tc.getControllerDeploymentName()
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Namespace: tc.OperatorNamespace,
			Name:      controllerDeployment,
		}),
		WithListOptions(&client.ListOptions{
			Namespace:     tc.OperatorNamespace,
			LabelSelector: selector,
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

// validatePodLogsForErrors checks pod logs for critical errors with retry mechanism.
func (tc *OperatorTestCtx) validatePodHealth(t *testing.T, selector labels.Selector) {
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
			conditions, found, _ := unstructured.NestedSlice(pod.Object, "status", "conditions")
			if !found {
				return false
			}

			isReady := false
			for _, condition := range conditions {
				if conditionMap, ok := condition.(map[string]interface{}); ok {
					if conditionType, _ := conditionMap["type"].(string); conditionType == "Ready" {
						if status, _ := conditionMap["status"].(string); status == "True" {
							isReady = true
							break
						}
					}
				}
			}

			if !isReady {
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
func (tc *OperatorTestCtx) validateSystemHealth(t *testing.T) {
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
