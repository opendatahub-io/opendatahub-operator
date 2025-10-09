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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
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
		{"Validate components deployment failure", resilienceTestCtx.ValidateComponentsDeploymentFailure},
		{"Validate missing CRD handling", resilienceTestCtx.ValidateMissingComponentsCRDHandling},
		{"Validate RBAC restriction handlings", resilienceTestCtx.ValidateRBACRestrictionHandlings},
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
	tc.validateDeploymentHealth(t)
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

	// Ensure clean initial state by disabling all components first
	t.Log("Ensuring clean initial state - disabling all components")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(updateAllComponentsTransform(components, operatorv1.Removed)),
	)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(`any(.status.conditions[]; .type == "%s" and .status == "%s")`, status.ConditionTypeComponentsReady, metav1.ConditionTrue)),
		WithCustomErrorMsg("All components should be cleanly removed before quota test"),
	)

	t.Log("Creating zero-pod quota (blocks everything)")
	tc.createZeroPodQuotaForOperator()

	allControllers := slices.Concat(
		slices.Collect(maps.Values(componentToControllerMap)),
		slices.Collect(maps.Values(internalComponentToControllerMap)),
	)

	t.Log("Rollout deployments restart to trigger resource quota if pod already exists")
	tc.rolloutDeployments(t, allControllers)

	t.Log("Enabling all components in DataScienceCluster")
	tc.EventuallyResourcePatched(
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
				`any(.status.conditions[]; 
            .type == "%s" and .status == "%s" and 
            (.message as $msg | %s | all(.[]; ($msg | contains(.)))))`,
				status.ConditionTypeComponentsReady,
				metav1.ConditionFalse,
				expectedMsgComponents,
			),
		),
	)

	t.Log("Disabling all components and verifying no managed components are reported")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(updateAllComponentsTransform(components, operatorv1.Removed)),
	)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(
			`.status.conditions[] | select(.type == "%s" and .status == "%s") | .message | test("nomanagedcomponents"; "i")`,
			status.ConditionTypeComponentsReady,
			metav1.ConditionTrue,
		)),
		WithCustomErrorMsg("All components should be cleanly removed"),
	)

	t.Log("Cleaning up restrictive quota")
	tc.deleteZeroPodQuotaForOperator()
}

func (tc *OperatorResilienceTestCtx) ValidateMissingComponentsCRDHandling(t *testing.T) {
	t.Helper()

	crdTestingName := "dashboards.components.platform.opendatahub.io"
	crd := tc.FetchResource(
		WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: crdTestingName}),
	)

	if crd == nil {
		t.Skipf("CRD %s not found, skipping test", crdTestingName)
		return
	}

	// Save a backup copy of the CRD
	crdBackup := resources.StripServerMetadata(crd)

	// Delete the CRD
	tc.DeleteResource(
		WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{
			Name: crd.GetName(),
		}),
		WithWaitForDeletion(true),
	)

	// Validate pod health and system health
	componentKind, _, _ := unstructured.NestedString(crd.Object, "spec", "names", "kind")
	componentName := strings.ToLower(componentKind)

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(
			testf.Transform(`.spec.components.%s.managementState = "%s"`, componentName, operatorv1.Managed),
		),
		WithCondition(jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, operatorv1.Managed)),
	)

	// Verify the system is unhealthy
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(And(
			jq.Match(`.status.conditions[] 
			| select(.type == "%s") 
			| .status == "%s"`, "Ready", metav1.ConditionFalse),
			jq.Match(`.status.conditions[] 
			| select(.type == "%s") 
			| .status == "%s"`, "ProvisioningSucceeded", metav1.ConditionFalse),
			jq.Match(`.status.conditions[] 
			| select(.type == "%s") 
			| .status == "%s"`, "ComponentsReady", metav1.ConditionFalse),
			jq.Match(`.status.conditions[] 
			| select(.type == "%s") 
			| .status == "%s"`, componentKind+"Ready", metav1.ConditionFalse),
		)),
		WithCustomErrorMsg("DSC should be unhealthy due to missing CRD"),
	)

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(
			testf.Transform(`.spec.components.%s.managementState = "%s"`, componentName, operatorv1.Removed),
		),
		WithCondition(jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, operatorv1.Removed)),
	)

	// Manually restore the CRD from backup
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(crdBackup),
		WithCustomErrorMsg("Failed to restore CRD from backup"),
	)
	tc.validateSystemHealth(t)
}

func (tc *OperatorResilienceTestCtx) ValidateRBACRestrictionHandlings(t *testing.T) {
	t.Helper()

	// Get the predictable ServiceAccount name based on deployment name
	deploymentName := tc.getControllerDeploymentName()
	expectedSAName := deploymentName

	// Find the ClusterRoleBinding that references our ServiceAccount
	crbs := tc.FetchResources(
		WithMinimalObject(gvk.ClusterRoleBinding, types.NamespacedName{}),
	)
	var crbName string
	for _, obj := range crbs {
		subjects, found, _ := unstructured.NestedSlice(obj.Object, "subjects")
		if !found {
			continue
		}
		// Check if any subject matches our ServiceAccount
		for _, subject := range subjects {
			if subj, ok := subject.(map[string]interface{}); ok {
				if name, _ := subj["name"].(string); name == expectedSAName {
					crbName = obj.GetName()
					break
				}
			}
		}
		if crbName != "" {
			break
		}
	}

	// EnsureResourceExists returns the object directly!
	crb := tc.EnsureResourceExists(
		WithMinimalObject(gvk.ClusterRoleBinding, types.NamespacedName{Name: crbName}),
		WithCustomErrorMsg(fmt.Sprintf("ClusterRoleBinding %s must exist for RBAC test", crbName)),
	)

	// Save a backup copy of the CRB
	crbBackup := resources.StripServerMetadata(crb)

	// Verify operator is initially healthy
	tc.validateDeploymentHealth(t)

	// Deleting ClusterRoleBinding to simulate RBAC restriction
	tc.DeleteResource(
		WithMinimalObject(gvk.ClusterRoleBinding, types.NamespacedName{Name: crbName}),
	)

	// Restart operator pods to pick up RBAC changes
	tc.rolloutDeployment(t, types.NamespacedName{Namespace: tc.OperatorNamespace, Name: deploymentName})

	// Verify opertor becomes unhealthy
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Namespace: tc.OperatorNamespace,
			Name:      deploymentName,
		}),
		WithCondition(jq.Match(`.status.readyReplicas != %d`, expectedReplicas)),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithCustomErrorMsg("Operator should fail without ClusterRoleBinding"),
	)

	// Create CRB from backup
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(crbBackup),
		WithCustomErrorMsg("Failed to restore ClusterRoleBinding from backup"),
	)

	// Verify operator recovers
	tc.validateDeploymentHealth(t)
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
			          "exceeded quota: %s|failed quota: %s|forbidden"; "i"
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

	// First, ensure the ResourceQuota is created
	tc.EventuallyResourceCreated(WithObjectToCreate(quota))

	// Then, wait for the ResourceQuota to become active (status populated by controller)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ResourceQuota, types.NamespacedName{
			Namespace: tc.AppsNamespace,
			Name:      restrictiveQuotaName,
		}),
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

func (tc *OperatorResilienceTestCtx) rolloutDeployment(t *testing.T, nn types.NamespacedName) {
	t.Helper()

	t.Logf("Triggering rollout restart for deployment: %s", nn.Name)
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.Deployment, nn),
		WithMutateFunc(testf.Transform(`.spec.template.metadata.annotations["kubectl.kubernetes.io/restartedAt"] = "%s"`, time.Now().Format(time.RFC3339))),
		WithIgnoreNotFound(true),
	)
}

func (tc *OperatorResilienceTestCtx) rolloutDeployments(t *testing.T, allControllers []string) {
	t.Helper()

	for _, deployment := range allControllers {
		tc.rolloutDeployment(t, types.NamespacedName{Namespace: tc.AppsNamespace, Name: deployment})
	}
}
