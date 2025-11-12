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
		{"Validate components deployment success", resilienceTestCtx.ValidateComponentsDeploymentSuccess},
		{"Validate components deployment failure", resilienceTestCtx.ValidateComponentsDeploymentFailure},
		{"Validate missing CRD handling", resilienceTestCtx.ValidateMissingComponentsCRDHandling},
		{"Validate RBAC restriction handling", resilienceTestCtx.ValidateRBACRestrictionHandling},
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

func (tc *OperatorResilienceTestCtx) ValidateComponentsDeploymentSuccess(t *testing.T) {
	t.Helper()

	componentName := componentApi.DashboardComponentName

	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, componentName, operatorv1.Managed)),
		WithCondition(And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeComponentsReady, metav1.ConditionTrue),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, metav1.ConditionTrue),
		)),
	)
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
		componentApi.LlamaStackOperatorComponentName:   "llama-stack-k8s-operator-controller-manager",
		componentApi.ModelRegistryComponentName:        "model-registry-operator-controller-manager",
		componentApi.RayComponentName:                  "kuberay-operator",
		componentApi.TrainingOperatorComponentName:     "kubeflow-training-operator",
		componentApi.TrainerComponentName:              "trainer-controller-manager",
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
	// Kueue is excluded because it does not have any deployment to manage anymore
	excludedComponents := 2 // TrustyAI and Kueue
	expectedTestableComponents := expectedComponentCount - excludedComponents
	tc.g.Expect(componentsLength).Should(Equal(expectedTestableComponents),
		"allComponents list is out of sync with DSC Components struct. "+
			"Expected %d testable components but found %d. "+
			"(Total DSC components: %d, Excluded: %d - TrustyAI due to InferenceServices CRD dependency and Kueue because dose not manage any deployment)",
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

func (tc *OperatorResilienceTestCtx) ValidateRBACRestrictionHandling(t *testing.T) {
	t.Helper()

	// Get the predictable ServiceAccount name based on deployment name
	deploymentName := tc.getControllerDeploymentName()

	operatorDeployment := tc.FetchResource(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.OperatorNamespace, Name: tc.getControllerDeploymentName()}),
	)
	tc.g.Expect(operatorDeployment).NotTo(BeNil(), "Operator deployment not found")

	// Find the ClusterRoleBinding that references our ServiceAccount
	crbBackups, crbNames := tc.findAndBackupAllCRBsForServiceAccount(operatorDeployment)
	if len(crbBackups) == 0 {
		t.Fatalf("No ClusterRoleBinding found for ServiceAccount %s", deploymentName)
	}

	// Verify operator is initially healthy
	tc.validateDeploymentHealth(t)

	// Deleting all ClusterRoleBinding to simulate RBAC restriction
	t.Log("Deleting all ClusterRoleBinding")
	for _, crbName := range crbNames {
		tc.DeleteResource(WithMinimalObject(gvk.ClusterRoleBinding, types.NamespacedName{Name: crbName}))
	}

	// Extract the pods label name from operator deployment labels
	operatorName, ok := operatorDeployment.GetLabels()["name"]
	tc.g.Expect(ok).To(BeTrue(), "name not found in operator deployment")

	// Delete the Operator Pods individually (API doesn't support bulk pod deletion)
	t.Log("Deleting operator Pods to force a restart")

	// First, fetch the pods that match our criteria
	pods := tc.FetchResources(
		WithMinimalObject(gvk.Pod, types.NamespacedName{Namespace: tc.OperatorNamespace}),
		WithListOptions(&client.ListOptions{
			Namespace: tc.OperatorNamespace,
			LabelSelector: labels.SelectorFromSet(map[string]string{
				"name": operatorName,
			}),
		}),
	)

	// Delete each pod individually using DeleteResource
	for _, pod := range pods {
		tc.DeleteResource(
			WithMinimalObject(gvk.Pod, types.NamespacedName{
				Namespace: pod.GetNamespace(),
				Name:      pod.GetName(),
			}),
			WithWaitForDeletion(true),
		)
	}

	// Verify operator becomes unhealthy
	t.Log("Verifying operator becomes unhealthy")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Namespace: tc.OperatorNamespace,
			Name:      deploymentName,
		}),
		WithCondition(jq.Match(`.status.readyReplicas != %d`, expectedReplicas)),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithCustomErrorMsg("Operator should fail without ClusterRoleBinding"),
	)

	// Restore all ClusterRoleBinding from backups
	t.Log("Restoring all ClusterRoleBinding")
	for _, crbBackup := range crbBackups {
		tc.EventuallyResourceCreatedOrUpdated(WithObjectToCreate(crbBackup))
	}

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

// verifyDeploymentsStuckDueToQuota validates that deployments are stuck due to resource quota restrictions.
func (tc *OperatorResilienceTestCtx) verifyDeploymentsStuckDueToQuota(t *testing.T, allControllers []string) {
	t.Helper()

	expectedCount := len(allControllers)

	// Then check that the matching deployments have 0 ready replicas
	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace}),
		WithCondition(jq.Match(`
			[.[] | select(
				(.metadata.name | test("%s"; "i")) or
				(.metadata.name | test("odh-(%s)"; "i"))
			) | select((.status.readyReplicas // 0) == 0)] |
        	length == %d
		`, strings.Join(allControllers, "|"), strings.Join(allControllers, "|"), expectedCount)),
		WithCustomErrorMsg(fmt.Sprintf("Expected all %d component deployments to have 0 ready replicas due to quota", expectedCount)),
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
		// Map datasciencepipelines to aipipelines for v2 API
		componentFieldName := component
		if component == dataSciencePipelinesComponentName {
			componentFieldName = aiPipelinesFieldName
		}
		transformParts[i] = fmt.Sprintf(`.spec.components.%s.managementState = "%s"`, componentFieldName, state)
	}

	return testf.Transform("%s", strings.Join(transformParts, " | "))
}

// findAndBackupAllCRBsForServiceAccount finds all ClusterRoleBindings referencing the given ServiceAccount and returns backup copies with their names.
func (tc *OperatorResilienceTestCtx) findAndBackupAllCRBsForServiceAccount(operatorDeployment *unstructured.Unstructured) ([]*unstructured.Unstructured, []string) {
	crbs := tc.FetchResources(
		WithMinimalObject(gvk.ClusterRoleBinding, types.NamespacedName{}),
	)

	saName, ok, err := unstructured.NestedString(operatorDeployment.Object, "spec", "template", "spec", "serviceAccountName")
	tc.g.Expect(err).NotTo(HaveOccurred(), "Failed to get serviceAccountName from operator deployment")
	tc.g.Expect(ok).To(BeTrue(), "serviceAccountName not found in operator deployment")

	var crbBackups []*unstructured.Unstructured
	var crbNames []string

	for _, obj := range crbs {
		subjects, found, _ := unstructured.NestedSlice(obj.Object, "subjects")
		if !found {
			continue
		}

		// Check if any subject matches our ServiceAccount
		for _, subject := range subjects {
			subj, ok := subject.(map[string]interface{})
			if !ok {
				continue
			}

			kind, _ := subj["kind"].(string)
			if kind != gvk.ServiceAccount.Kind {
				continue
			}

			namespace, _ := subj["namespace"].(string)
			if namespace != tc.OperatorNamespace {
				continue
			}

			if name, _ := subj["name"].(string); name == saName {
				crbNames = append(crbNames, obj.GetName())
				crbBackups = append(crbBackups, resources.StripServerMetadata(&obj))
				break
			}
		}
	}

	return crbBackups, crbNames
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
