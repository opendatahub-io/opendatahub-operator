package e2e_test

import (
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

type OperatorResilienceTestCtx struct {
	*TestContext
}

const (
	expectedReplicas     = 3                        // Number of replicas for the deployment
	restrictiveQuotaName = "test-restrictive-quota" // Restrictive quota name
)

// operatorResilienceTestSuite runs operator resilience and failure recovery tests.
func operatorResilienceTestSuite(t *testing.T) {
	t.Helper()

	// Initialize the test context.
	tc, err := NewTestContext(t)
	require.NoError(t, err, "Failed to initialize test context")

	// Create an instance of resilience test context.
	resilienceTestCtx := OperatorResilienceTestCtx{
		TestContext: tc,
	}

	// Define resilience test cases.
	testCases := []TestCase{
		{"Validate operator deployment health", resilienceTestCtx.ValidateOperatorDeployment},
		{"Validate leader election behavior", resilienceTestCtx.ValidateLeaderElectionBehavior},
		{"Validate pod recovery resilience", resilienceTestCtx.ValidatePodRecoveryResilience},
		{"Validate components deployment failure", resilienceTestCtx.ValidateComponentsDeploymentFailure},
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

// ValidateComponentsDeploymentFailure simulates component deployment failure using restrictive resource quota.
func (tc *OperatorResilienceTestCtx) ValidateComponentsDeploymentFailure(t *testing.T) {
	t.Helper()

	// To handle upstream/downstream i trimmed prefix(odh) from few controller names
	componentToControllerMap := map[string]string{
		componentApi.DashboardComponentName:            "dashboard",
		componentApi.DataSciencePipelinesComponentName: "data-science-pipelines-operator-controller-manager",
		componentApi.FeastOperatorComponentName:        "feast-operator-controller-manager",
		componentApi.KserveComponentName:               "kserve-controller-manager",
		componentApi.KueueComponentName:                "kueue-controller-manager",
		componentApi.LlamaStackOperatorComponentName:   "llama-stack-k8s-operator-controller-manager",
		componentApi.ModelMeshServingComponentName:     "modelmesh-controller",
		componentApi.ModelRegistryComponentName:        "model-registry-operator-controller-manager",
		componentApi.RayComponentName:                  "kuberay-operator",
		componentApi.TrainingOperatorComponentName:     "kubeflow-training-operator",
		// componentApi.TrustyAIComponentName:             "trustyai-service-operator-controller-manager",
		componentApi.WorkbenchesComponentName: "notebook-controller-manager",
	}

	// Error message includes components + internal components name
	var internalComponentToControllerMap = map[string]string{
		componentApi.ModelControllerComponentName: "model-controller",
	}

	components := slices.Collect(maps.Keys(componentToControllerMap))
	componentsLength := len(components)

	t.Log("Verifying component count matches DSC Components struct")

	expectedComponentCount := reflect.TypeOf(dscv2.Components{}).NumField()
	// TrustyAI is excluded from quota failure testing due to InferenceServices CRD dependency
	excludedComponents := 1 // TrustyAI
	expectedTestableComponents := expectedComponentCount - excludedComponents
	tc.g.Expect(componentsLength).Should(Equal(expectedTestableComponents),
		"allComponents list is out of sync with DSC Components struct. "+
			"Expected %d testable components but found %d. "+
			"(Total DSC components: %d, Excluded: %d - TrustyAI due to InferenceServices CRD dependency)",
		expectedTestableComponents, componentsLength, expectedComponentCount, excludedComponents)

	t.Log("Creating zero-pod quota (blocks everything)")
	tc.createZeroPodQuotaForOperator()

	allControllers := slices.Concat(
		slices.Collect(maps.Values(componentToControllerMap)),
		slices.Collect(maps.Values(internalComponentToControllerMap)),
	)

	t.Log("Rollout deployments restart to trigger resource quota if pod already exists")
	tc.rolloutDeployments(t, allControllers)

	t.Log("Enabling all components in DataScienceCluster")
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(updateAllComponentsTransform(components, operatorv1.Managed)),
	)

	t.Log("Verifying component deployments are stuck due to quota")
	tc.verifyDeploymentsStuckDueToQuota(t, allControllers)

	t.Log("Verifying DSC reports all failed components")

	allComponents := slices.Concat(
		components,
		slices.Collect(maps.Keys(internalComponentToControllerMap)),
	)
	sort.Strings(allComponents)
	expectedMsgComponents := fmt.Sprintf(`["%s"]`, strings.Join(allComponents, `","`))
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(
			jq.Match(
				`.status.conditions[]
				| select(.type == "ComponentsReady" and .status == "False")
				| .message as $m
				 | (%s | all(.[]; ($m | contains(.))))`,
				expectedMsgComponents,
			),
		),
	)

	t.Log("Disabling all components and verifying no managed components are reported")
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(updateAllComponentsTransform(components, operatorv1.Removed)),
	)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(
			`.status.conditions[]
			| select(.type == "ComponentsReady" and .status == "%s")
			| .message
			| test("nomanagedcomponents"; "i")`,
			metav1.ConditionTrue,
		)),
	)

	t.Log("Cleaning up restrictive quota")
	tc.deleteZeroPodQuotaForOperator()
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
					if conditionType == status.ConditionTypeReady && conditionStatus == string(metav1.ConditionTrue) {
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
						if rc, found := statusMap["restartCount"]; found {
							// Use reflection to handle any numeric type
							v := reflect.ValueOf(rc)
							if v.Kind() >= reflect.Int && v.Kind() <= reflect.Float64 {
								if v.Convert(reflect.TypeOf(float64(0))).Float() > 0 {
									t.Logf("Warning: Pod %s has restart count: %v", pod.GetName(), rc)
								}
							} else {
								// best-effort log; do not fail health
								t.Logf("Warning: Pod %s restartCount has unexpected type %T: %v", pod.GetName(), rc, rc)
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

// verifyDeploymentsStuckDueToQuota validates that deployments fail with quota error messages.
func (tc *OperatorResilienceTestCtx) verifyDeploymentsStuckDueToQuota(t *testing.T, allControllers []string) {
	t.Helper()

	expectedCount := len(allControllers)

	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace}),
		WithCondition(jq.Match("%s", fmt.Sprintf(`
			map(
				select(.metadata.name | test("%s"; "i")) |
				select(
					.status.conditions[]? |
					select(.type == "ReplicaFailure") |
					select(.status == "True") |
			        select(.message | test(
			          "forbidden: exceeded quota: %s|forbidden: failed quota: %s|forbidden"; "i"
			        ))
				)
			) |
			length == %d
		`, strings.Join(allControllers, "|"), restrictiveQuotaName, restrictiveQuotaName, expectedCount))),
		WithCustomErrorMsg(fmt.Sprintf("Expected all %d component deployments to have quota error messages", expectedCount)),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
	)
}

// createZeroPodQuotaForOperator creates a ResourceQuota that completely blocks all pod creation.
// This ensures no pods can start before quota enforcement, eliminating race conditions.
func (tc *OperatorResilienceTestCtx) createZeroPodQuotaForOperator() {
	quota := &corev1.ResourceQuota{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gvk.ResourceQuota.Version,
			Kind:       gvk.ResourceQuota.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      restrictiveQuotaName,
			Namespace: tc.AppsNamespace,
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				// Only count pods - much faster than CPU/memory calculations
				corev1.ResourcePods: resource.MustParse("0"),
			},
		},
	}

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(quota),
		WithCondition(jq.Match(`.status.hard != null`)),
		WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
		WithCustomErrorMsg("ResourceQuota should be active with hard limits set"),
	)
}

func (tc *OperatorResilienceTestCtx) deleteZeroPodQuotaForOperator() {
	tc.DeleteResource(
		WithMinimalObject(
			gvk.ResourceQuota,
			types.NamespacedName{Namespace: tc.AppsNamespace, Name: restrictiveQuotaName},
		),
		WithIgnoreNotFound(true),
		WithWaitForDeletion(true),
	)
}

// updateAllComponentsTransform creates a transform function to update all component management states.
func updateAllComponentsTransform(components []string, state operatorv1.ManagementState) testf.TransformFn {
	transformParts := make([]string, len(components))
	for i, component := range components {
		transformParts[i] = fmt.Sprintf(`.spec.components.%s.managementState = "%s"`, component, state)
	}

	return testf.Transform("%s", strings.Join(transformParts, " | "))
}

func (tc *OperatorResilienceTestCtx) rolloutDeployments(t *testing.T, allControllers []string) {
	t.Helper()

	existingDeployments := tc.FetchResources(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace}),
		WithListOptions(&client.ListOptions{Namespace: tc.AppsNamespace}),
	)

	deploymentExists := make(map[string]bool)
	for _, dep := range existingDeployments {
		deploymentExists[dep.GetName()] = true
	}

	for _, deployment := range allControllers {
		if deploymentExists[deployment] {
			t.Logf("Triggering rollout restart for deployment: %s", deployment)
			tc.EventuallyResourceCreatedOrUpdated(
				WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace, Name: deployment}),
				WithMutateFunc(testf.Transform(`.spec.template.metadata.annotations["kubectl.kubernetes.io/restartedAt"] = "%s"`, time.Now().Format(time.RFC3339))),
			)
		}
	}
}
