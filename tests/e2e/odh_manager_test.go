package e2e_test

import (
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
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
	controllerDeployment = "opendatahub-operator-controller-manager"
	expectedReplicas     = 3
)

func odhOperatorTestSuite(t *testing.T) {
	t.Helper()

	// Initialize the test context.
	tc, err := NewTestContext(t)
	require.NoError(t, err, "Failed to initialize test context")

	// Create an instance of test context.
	operatorTestCtx := OperatorTestCtx{
		TestContext: tc,
	}

	// Define test cases.
	testCases := []TestCase{
		{name: "Validate ODH Operator pod", testFn: operatorTestCtx.testODHDeployment},
		{name: "Validate CRDs owned by the operator", testFn: operatorTestCtx.ValidateOwnedCRDs},
		{name: "Validate multi replica leader election and recovery", testFn: operatorTestCtx.ValidateMultiReplicaLeaderElectionAndRecovery},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// testODHDeployment checks if the ODH deployment exists and is correctly configured.
func (tc *OperatorTestCtx) testODHDeployment(t *testing.T) {
	t.Helper()

	// Verify if the operator deployment is created
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

// Scale the operator for multiple replicas, validate pod leader election and behavior.
func (tc *OperatorTestCtx) ValidateMultiReplicaLeaderElectionAndRecovery(t *testing.T) {
	t.Helper()

	selector := labels.SelectorFromSet(labels.Set{
		"control-plane": "controller-manager",
	})
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(CreateDSCI(tc.DSCInitializationNamespacedName.Name, tc.AppsNamespace)),
		WithCondition(jq.Match(`.status.phase == "%s"`, status.ConditionTypeReady)),
		WithCustomErrorMsg("Failed to create DSCInitialization resource %s", tc.DSCInitializationNamespacedName.Name),
	)

	// Find and delete the current leader pod
	leaderPodName := tc.findLeaderPodFromLeases()
	require.NotEmpty(t, leaderPodName, "Failed to find leader pod from lease resources")

	// Delete it
	tc.DeleteResource(
		WithMinimalObject(gvk.Pod, types.NamespacedName{
			Name:      leaderPodName,
			Namespace: tc.OperatorNamespace,
		}),
	)

	// Create DSC
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(CreateDSC(tc.DataScienceClusterNamespacedName.Name)),
		WithCondition(jq.Match(`.status.phase == "%s"`, status.ConditionTypeReady)),
		WithCustomErrorMsg("Failed to create DataScienceCluster resource %s", tc.DataScienceClusterNamespacedName.Name),
	)

	// Validate DSC Duplication
	dup := CreateDSC(dscInstanceNameDuplicate)
	tc.EnsureResourceIsUnique(dup, "Error validating DataScienceCluster duplication")

	// Verify new leader election
	newLeaderPod := tc.findLeaderPodFromLeases()
	require.NotEmpty(t, newLeaderPod, "Failed to verify new leader election")

	// Get and delete a another pod for recovery testing
	pods := tc.getOperatorPods(selector)
	require.NotEmpty(t, pods, "No controller manager pods found")

	tc.DeleteResource(
		WithMinimalObject(gvk.Pod, types.NamespacedName{
			Name:      pods[0].GetName(),
			Namespace: pods[0].GetNamespace(),
		}),
		WithWaitForDeletion(true),
	)

	// Wait for pod recovery and check logs - using tc.g.Eventually
	tc.g.Eventually(func() bool {
		currentPods := tc.getOperatorPods(selector)
		return len(currentPods) >= expectedReplicas
	}).Should(BeTrue(), "Pods should recover after deletion")

	// Check logs for errors with retry mechanism
	tc.validatePodLogsForErrors(t, selector)
	// Verify the deployment health
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
				jq.Match(`.spec.replicas == %d`, expectedReplicas),
				jq.Match(`.status.readyReplicas == %d`, expectedReplicas),
				jq.Match(`.status.availableReplicas == %d`, expectedReplicas),
			),
		),
		WithCustomErrorMsg(`Ensuring all the pods are running`),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithCondition(
			jq.Match(`.status.phase == "%s"`, status.ConditionTypeReady),
		),
		WithCustomErrorMsg("DSCI should be Ready"),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(
			jq.Match(`.status.phase == "%s"`, status.ConditionTypeReady),
		),
		WithCustomErrorMsg("DSC should be Ready"),
	)

	// Delete created DSC and DSCI
	tc.DeleteResource(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithWaitForDeletion(true),
	)

	tc.DeleteResource(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithWaitForDeletion(true),
	)
}

// findLeaderPodFromLeases finds the current leader pod from lease resources.
func (tc *OperatorTestCtx) findLeaderPodFromLeases() string {
	leases := tc.FetchResources(
		WithMinimalObject(gvk.Lease, types.NamespacedName{Namespace: tc.OperatorNamespace}),
		WithListOptions(&client.ListOptions{
			Namespace: tc.OperatorNamespace,
		}),
	)

	for _, lease := range leases {
		spec, ok := lease.Object["spec"].(map[string]interface{})
		if !ok {
			continue
		}
		holderIdentity, ok := spec["holderIdentity"].(string)
		if !ok {
			continue
		}
		if strings.Contains(holderIdentity, controllerDeployment) {
			return strings.Split(holderIdentity, "_")[0]
		}
	}
	return ""
}

// getOperatorPods fetches operator pods with given selector.
func (tc *OperatorTestCtx) getOperatorPods(selector labels.Selector) []unstructured.Unstructured {
	return tc.FetchResources(
		WithMinimalObject(gvk.Pod, types.NamespacedName{Namespace: tc.OperatorNamespace}),
		WithListOptions(&client.ListOptions{
			Namespace:     tc.OperatorNamespace,
			LabelSelector: selector,
		}),
	)
}

// validatePodLogsForErrors checks pod logs for critical error patterns using regex.
func (tc *OperatorTestCtx) validatePodLogsForErrors(t *testing.T, selector labels.Selector) {
	t.Helper()

	var logs string

	// Get logs with retry mechanism
	tc.g.Eventually(func() bool {
		pods := tc.getOperatorPods(selector)
		if len(pods) == 0 {
			return false
		}

		config, err := ctrl.GetConfig()
		if err != nil {
			return false
		}

		clientset, err := kubernetes.NewForConfig(config)
		if err != nil {
			return false
		}

		tailLines := int64(100)
		req := clientset.CoreV1().Pods(tc.OperatorNamespace).GetLogs(pods[0].GetName(), &corev1.PodLogOptions{
			Container: "manager",
			TailLines: &tailLines, // (Optional) Limit to recent logs for performance
		})

		logStream, err := req.Stream(tc.Context())
		if err != nil {
			return false
		}
		defer logStream.Close()

		logBytes, err := io.ReadAll(logStream)
		if err != nil {
			return false
		}

		logs = string(logBytes)
		return len(logs) > 0
	}).Should(BeTrue(), "Should retrieve logs successfully")

	// Check for critical error patterns - improved regex but simple logic
	errorPattern := regexp.MustCompile(`"level"\s*:\s*"[Ee]rror"|[Ff]ailed to reconcile|controller runtime error|panic:|fatal error:`)
	hasErrors := errorPattern.MatchString(logs)
	tc.g.Expect(hasErrors).To(BeFalse(), "Found critical error patterns in logs")
}
