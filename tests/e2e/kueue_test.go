package e2e_test

import (
	"strings"
	"testing"

	gTypes "github.com/onsi/gomega/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/kueue"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const (
	kueueOcpOperatorNamespace           = "openshift-kueue-operator" // Namespace for the Kueue Operator
	kueueOcpOperatorChannel             = "stable-v1.1"
	kueueTestManagedNamespace           = "test-kueue-managed-ns"
	kueueTestLegacyManagedNamespace     = "test-legacy-kueue-managed-ns"
	kueueTestWebhookNonManagedNamespace = "test-kueue-webhook-non-managed-ns"
	kueueTestHardwareProfileNamespace   = "test-kueue-hardware-profile-ns"
	kueueDefaultClusterQueueName        = "default"
	kueueDefaultLocalQueueName          = "default"
)

var (
	KueueManagedLabels = map[string]string{
		cluster.KueueManagedLabelKey: "true",
	}

	KueueLegacyManagedLabels = map[string]string{
		cluster.KueueLegacyManagedLabelKey: "true",
	}
)

type KueueTestCtx struct {
	*ComponentTestCtx
}

func kueueTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(t, &componentApi.Kueue{})
	require.NoError(t, err)

	componentCtx := KueueTestCtx{
		ComponentTestCtx: ct,
	}

	// Define core test cases
	testCases := []TestCase{
		{"Validate component unmanaged error with ocp kueue-operator not installed", componentCtx.ValidateKueueUnmanagedWithoutOcpKueueOperator},
		{"Validate component removed to unmanaged transition", componentCtx.ValidateKueueRemovedToUnmanagedTransition},
		{"Validate component unmanaged to removed transition", componentCtx.ValidateKueueUnmanagedToRemovedTransition},
	}

	// Add webhook tests if enabled
	if testOpts.webhookTest {
		testCases = append(testCases,
			TestCase{"Validate webhook validations", componentCtx.ValidateWebhookValidations},
		)
	}

	// Always run deletion recovery and component disable tests last
	testCases = append(testCases,
		TestCase{"Validate resource deletion recovery", componentCtx.ValidateAllDeletionRecovery},
		TestCase{"Validate component disabled", componentCtx.ValidateKueueComponentDisabled})

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateKueueUnmanagedWithoutOcpKueueOperator ensures that if the component is in Unmanaged state and ocp kueue operator is not installed, then its status is "Not Ready".
func (tc *KueueTestCtx) ValidateKueueUnmanagedWithoutOcpKueueOperator(t *testing.T) {
	t.Helper()

	componentName := strings.ToLower(tc.GVK.Kind)

	// since the test may be executed on a non-clean state, let clean it up
	// so first set the component as removed ...
	tc.UpdateComponentStateInDataScienceCluster(operatorv1.Removed)

	// ... and then cleanup tests resources
	cleanupKueueTestResources(t, tc.TestContext)

	stateUnmanaged := operatorv1.Unmanaged

	// State must be Unmanaged, Ready condition must be false because ocp kueue-operator is installed
	conditionsNotReady := []gTypes.GomegaMatcher{
		// Validate that the component's management state is updated correctly
		jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, stateUnmanaged),

		// Validate the "Ready" condition for the component
		jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionFalse),
	}

	tc.ConsistentlyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, componentName, stateUnmanaged)),
		WithCondition(And(conditionsNotReady...)),
	)
}

// ValidateComponentEnabled ensures the transition between Removed and Unmanaged state happens as expected.
func (tc *KueueTestCtx) ValidateKueueRemovedToUnmanagedTransition(t *testing.T) {
	t.Helper()

	componentName := strings.ToLower(tc.GVK.Kind)

	// since the test may be executed on a non-clean state, let clean it up
	// so first set the component as removed ...
	tc.UpdateComponentStateInDataScienceCluster(operatorv1.Removed)

	// ... and then cleanup tests resources
	cleanupKueueTestResources(t, tc.TestContext)

	// Create a test namespace with Kueue management annotation
	tc.setupNamespace(kueueTestManagedNamespace, KueueManagedLabels)

	// UNMANAGED
	stateUnmanaged := operatorv1.Unmanaged
	conditionsUnmanagedNotReady := []gTypes.GomegaMatcher{
		// Validate that the component's management state is updated correctly
		jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, stateUnmanaged),

		// Validate the "Ready" condition for the component
		jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionFalse),
	}

	// Create Kueue ConfigMap to verify default Kueue resource is created correctly
	tc.createKueueConfigMap(t)

	// Update the management state of the component in the DataScienceCluster.
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, componentName, stateUnmanaged)),
		WithCondition(And(conditionsUnmanagedNotReady...)),
	)

	// Install ocp kueue-operator
	tc.EnsureOperatorInstalledWithGlobalOperatorGroupAndChannel(types.NamespacedName{Name: kueueOpName, Namespace: kueueOcpOperatorNamespace}, kueueOcpOperatorChannel)

	conditionsUnmanagedReady := []gTypes.GomegaMatcher{
		// Validate that the component's management state is updated correctly
		jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, stateUnmanaged),
		// Validate the "Ready" condition for the component
		jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionTrue),
	}

	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(And(conditionsUnmanagedReady...)),
	)

	// Validate that Kueue configuration is created with all expected frameworks
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.KueueConfigV1, types.NamespacedName{Name: kueue.KueueCRName}),
		WithCondition(And(
			jq.Match(`([
				"BatchJob", "Deployment", "JobSet", "LeaderWorkerSet", "MPIJob",
				"PaddleJob", "Pod", "PyTorchJob", "RayCluster", "RayJob", "StatefulSet", "TFJob",
				"XGBoostJob"
			] - .spec.config.integrations.frameworks) | length == 0`),
		)),
		WithEventuallyTimeout(tc.TestTimeouts.shortEventuallyTimeout),
	)

	// Default resources should be created
	tc.ensureClusterAndLocalQueueExist(kueueTestManagedNamespace)

	// Validate that default resource flavor is created
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ResourceFlavor, types.NamespacedName{Name: kueue.DefaultFlavorName}),
	)

	tc.DeleteResource(
		WithMinimalObject(gvk.ConfigMap, types.NamespacedName{Name: kueue.KueueConfigMapName, Namespace: tc.AppsNamespace}),
		WithIgnoreNotFound(true),
		WithWaitForDeletion(true),
	)
}

// ValidateKueueUnmanagedToRemovedTransition ensures the transition from Unmanaged to Removed state happens as expected.
func (tc *KueueTestCtx) ValidateKueueUnmanagedToRemovedTransition(t *testing.T) {
	t.Helper()

	componentName := strings.ToLower(tc.GVK.Kind)

	// since the test may be executed on a non-clean state, let clean it up
	// so first set the component as removed ...
	tc.UpdateComponentStateInDataScienceCluster(operatorv1.Removed)

	// ... and then cleanup tests resources
	cleanupKueueTestResources(t, tc.TestContext)

	// UNMANAGED
	// Install ocp kueue-operator
	tc.EnsureOperatorInstalledWithGlobalOperatorGroupAndChannel(types.NamespacedName{Name: kueueOpName, Namespace: kueueOcpOperatorNamespace}, kueueOcpOperatorChannel)
	stateUnmanaged := operatorv1.Unmanaged
	conditionsUnmanaged := []gTypes.GomegaMatcher{
		// Validate that the component's management state is updated correctly
		jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, stateUnmanaged),
		// Validate the "Ready" condition for the component
		jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionTrue),
	}

	// Update the management state of the component in the DataScienceCluster.
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, componentName, stateUnmanaged)),
		WithCondition(And(conditionsUnmanaged...)),
	)

	// Create a test namespace with Kueue management annotation
	tc.setupNamespace(kueueTestManagedNamespace, KueueManagedLabels)

	// Default resources should be created
	tc.ensureClusterAndLocalQueueExist(kueueTestManagedNamespace)

	// Validate that default resource flavor is created
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ResourceFlavor, types.NamespacedName{Name: kueue.DefaultFlavorName}),
	)

	// REMOVED
	stateRemoved := operatorv1.Removed
	conditionsRemovedReady := []gTypes.GomegaMatcher{
		// Validate that the component's management state is updated correctly
		jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, stateRemoved),
		// Validate the "Ready" condition for the component
		jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionTrue),
	}

	// Update the management state of the component in the DataScienceCluster to Removed.
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, componentName, stateRemoved)),
		WithCondition(And(conditionsRemovedReady...)),
	)

	// Validate default resources are still there
	tc.ensureClusterAndLocalQueueExist(kueueTestManagedNamespace)

	// Remove Kueue test resources
	cleanupKueueTestResources(t, tc.TestContext)
}

// ValidateWebhookValidations runs both Kueue and hardware profile webhook validation tests
// with proper Workbenches component setup/teardown.
func (tc *KueueTestCtx) ValidateWebhookValidations(t *testing.T) {
	t.Helper()

	// Enable Workbenches component to ensure Notebook CRD is available for webhook tests
	// This is required because webhook tests use Notebook objects which need the Notebook CRD
	// installed by the Workbenches component
	tc.UpdateComponentStateInDataScienceClusterWithKind(operatorv1.Managed, componentApi.WorkbenchesKind)

	// Run webhook validation tests as subtests
	t.Run("Kueue webhook validation", tc.ValidateKueueWebhookValidation)
	t.Run("Hardware profile webhook validation", tc.ValidateHardwareProfileWebhookValidation)

	// Ensure Workbenches is disabled after tests, even if they fail
	tc.UpdateComponentStateInDataScienceClusterWithKind(operatorv1.Removed, componentApi.WorkbenchesKind)

	// Remove Kueue test resources
	cleanupKueueTestResources(t, tc.TestContext)
}

// ValidateKueueWebhookValidation validates the Kueue validating webhook behavior using table-driven tests.
func (tc *KueueTestCtx) ValidateKueueWebhookValidation(t *testing.T) {
	t.Helper()

	// Install ocp kueue-operator
	tc.EnsureOperatorInstalledWithGlobalOperatorGroupAndChannel(types.NamespacedName{Name: kueueOpName, Namespace: kueueOcpOperatorNamespace}, kueueOcpOperatorChannel)
	// Ensure Kueue is in Unmanaged state
	tc.UpdateComponentStateInDataScienceCluster(operatorv1.Unmanaged)

	// Ensure the managed namespace exists
	tc.setupNamespace(kueueTestManagedNamespace, KueueManagedLabels)

	// Create a non-managed namespace for testing
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(CreateNamespaceWithLabels(kueueTestWebhookNonManagedNamespace, map[string]string{"test-type": "kqueue"})),
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: kueueTestWebhookNonManagedNamespace}),
		WithCustomErrorMsg("Failed to create non-managed test namespace"),
	)

	// Helper function to create and test notebook
	testNotebook := func(name, namespace, expectedError, errorMsg string, labels map[string]string, shouldBlock bool) func(*testing.T) {
		return func(t *testing.T) {
			t.Helper()

			// Create notebook with labels if provided
			notebook := envtestutil.NewNotebook(name, namespace, envtestutil.WithLabels(labels))

			// Handle blocking case first (exceptional path)
			if shouldBlock {
				tc.EnsureWebhookBlocksResourceCreation(
					WithObjectToCreate(notebook),
					WithInvalidValue(expectedError),
					WithCustomErrorMsg(errorMsg),
				)
				return
			}

			// Happy path - webhook allows the resource
			tc.EventuallyResourceCreatedOrUpdated(
				WithObjectToCreate(notebook),
				WithCustomErrorMsg(errorMsg),
			)
		}
	}

	// Define test cases
	testCases := []TestCase{
		{
			name: "blocks workload with missing queue label in managed namespace",
			testFn: testNotebook(
				"notebook-missing-queue",
				kueueTestManagedNamespace,
				cluster.KueueQueueNameLabel,
				"Expected notebook without Kueue queue label to be blocked",
				nil,
				true,
			),
		},
		{
			name: "blocks workload with empty queue label in managed namespace",
			testFn: testNotebook(
				"notebook-empty-queue",
				kueueTestManagedNamespace,
				"empty",
				"Expected notebook with empty Kueue queue label to be blocked",
				map[string]string{cluster.KueueQueueNameLabel: ""},
				true,
			),
		},
		{
			name: "allows workload without queue label in non-managed namespace",
			testFn: testNotebook(
				"notebook-non-managed",
				kueueTestWebhookNonManagedNamespace,
				"",
				"Expected notebook in non-managed namespace to be allowed",
				nil,
				false,
			),
		},
		{
			name: "allows workload with valid queue label in managed namespace",
			testFn: testNotebook(
				"notebook-valid-queue",
				kueueTestManagedNamespace,
				"",
				"Expected notebook with valid Kueue queue label to be allowed",
				map[string]string{cluster.KueueQueueNameLabel: "default"},
				false,
			),
		},
	}

	// Run the test cases
	RunTestCases(t, testCases)
}

// ValidateHardwareProfileWebhookValidation validates the hardware profile webhook behavior using table-driven tests.
func (tc *KueueTestCtx) ValidateHardwareProfileWebhookValidation(t *testing.T) {
	t.Helper()

	// Create a non-managed namespace for hardware profile testing (avoids Kueue validation interference)
	tc.setupNamespace(kueueTestHardwareProfileNamespace)

	// Helper struct for hardware profile test cases to reduce parameter count
	type HardwareProfileTestCase struct {
		name              string
		workloadName      string
		profileName       string
		profileSpec       *infrav1.HardwareProfileSpec
		shouldBlock       bool
		expectedError     string
		errorMsg          string
		expectedCondition gTypes.GomegaMatcher
	}

	// Common hardware profile specs for this test function
	basicProfile := &infrav1.HardwareProfileSpec{
		Identifiers: []infrav1.HardwareIdentifier{
			{
				DisplayName:  "CPU",
				Identifier:   "cpu",
				MinCount:     intstr.FromInt32(1),
				DefaultCount: intstr.FromInt32(2),
				ResourceType: "CPU",
			},
		},
	}

	resourceInjectionProfile := &infrav1.HardwareProfileSpec{
		Identifiers: []infrav1.HardwareIdentifier{
			{
				DisplayName:  "CPU",
				Identifier:   "cpu",
				MinCount:     intstr.FromInt32(2),
				DefaultCount: intstr.FromInt32(2),
				ResourceType: "CPU",
			},
			{
				DisplayName:  "Memory",
				Identifier:   "memory",
				MinCount:     intstr.FromString("4Gi"),
				DefaultCount: intstr.FromString("4Gi"),
				ResourceType: "Memory",
			},
			{
				DisplayName:  "GPU",
				Identifier:   "nvidia.com/gpu",
				MinCount:     intstr.FromInt32(1),
				DefaultCount: intstr.FromInt32(1),
				ResourceType: "Accelerator",
			},
		},
	}

	nodeSchedulingProfile := &infrav1.HardwareProfileSpec{
		Identifiers: []infrav1.HardwareIdentifier{
			{
				DisplayName:  "GPU",
				Identifier:   "nvidia.com/gpu",
				MinCount:     intstr.FromInt32(1),
				DefaultCount: intstr.FromInt32(1),
				ResourceType: "Accelerator",
			},
		},
		SchedulingSpec: &infrav1.SchedulingSpec{
			SchedulingType: infrav1.NodeScheduling,
			Node: &infrav1.NodeSchedulingSpec{
				NodeSelector: map[string]string{
					"accelerator": "nvidia-tesla-v100",
					"zone":        "us-west1-a",
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "nvidia.com/gpu",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
			},
		},
	}

	// Simplified helper function with struct parameter
	testHardwareProfileWorkload := func(testCase HardwareProfileTestCase) func(*testing.T) {
		return func(t *testing.T) {
			t.Helper()

			// Use the dedicated hardware profile test namespace (non-managed to avoid Kueue validation)
			testNamespace := kueueTestHardwareProfileNamespace

			// Create hardware profile if spec is provided
			if testCase.profileSpec != nil {
				hwp := envtestutil.NewHardwareProfile(testCase.profileName, testNamespace, envtestutil.WithHardwareProfileSpec(*testCase.profileSpec))
				tc.EventuallyResourceCreatedOrUpdated(
					WithObjectToCreate(hwp),
					WithCustomErrorMsg("Failed to create hardware profile for %s", testCase.name),
				)
			}

			// Create notebook workload
			notebook := envtestutil.NewNotebook(testCase.workloadName, testNamespace, envtestutil.WithHardwareProfile(testCase.profileName))

			// Handle blocking case first (exceptional path)
			if testCase.shouldBlock {
				tc.EnsureWebhookBlocksResourceCreation(
					WithObjectToCreate(notebook),
					WithInvalidValue(testCase.expectedError),
					WithCustomErrorMsg(testCase.errorMsg),
				)
				return
			}

			// Happy path - webhook allows the resource
			tc.EventuallyResourceCreatedOrUpdated(
				WithObjectToCreate(notebook),
				WithCustomErrorMsg(testCase.errorMsg),
				WithCondition(testCase.expectedCondition),
			)
		}
	}

	// Define test cases
	testCases := []TestCase{
		{
			name: "blocks workload with non-existent hardware profile",
			testFn: testHardwareProfileWorkload(
				HardwareProfileTestCase{
					name:              "non-existent",
					workloadName:      "notebook-invalid-profile",
					profileName:       "non-existent-profile",
					profileSpec:       nil,  // No hardware profile spec - profile doesn't exist
					shouldBlock:       true, // Should block
					expectedError:     "non-existent-profile",
					errorMsg:          "Expected notebook with non-existent hardware profile to be blocked by webhook",
					expectedCondition: nil,
				}),
		},
		{
			name: "allows workload with valid hardware profile",
			testFn: testHardwareProfileWorkload(
				HardwareProfileTestCase{
					name:              "valid",
					workloadName:      "notebook-valid-profile",
					profileName:       "basic-profile",
					profileSpec:       basicProfile,
					shouldBlock:       false, // Should allow
					expectedError:     "",
					errorMsg:          "Expected notebook with valid hardware profile to be allowed",
					expectedCondition: nil,
				}),
		},
		{
			name: "injects resources correctly",
			testFn: testHardwareProfileWorkload(
				HardwareProfileTestCase{
					name:          "resource-injection",
					workloadName:  "notebook-resource-injection",
					profileName:   "resource-profile",
					profileSpec:   resourceInjectionProfile,
					shouldBlock:   false, // Should allow
					expectedError: "",
					errorMsg:      "Failed to validate resource injection in notebook",
					expectedCondition: And(
						jq.Match(`.spec.template.spec.containers[0].resources.requests.cpu == "2"`),
						jq.Match(`.spec.template.spec.containers[0].resources.requests.memory == "4Gi"`),
						jq.Match(`.spec.template.spec.containers[0].resources.requests["nvidia.com/gpu"] == "1"`),
					),
				}),
		},
		{
			name: "injects node scheduling correctly",
			testFn: testHardwareProfileWorkload(
				HardwareProfileTestCase{
					name:          "node-scheduling",
					workloadName:  "notebook-node-scheduling",
					profileName:   "node-scheduling-profile",
					profileSpec:   nodeSchedulingProfile,
					shouldBlock:   false, // Should allow
					expectedError: "",
					errorMsg:      "Failed to validate node scheduling injection in notebook",
					expectedCondition: And(
						jq.Match(`.spec.template.spec.nodeSelector.accelerator == "nvidia-tesla-v100"`),
						jq.Match(`.spec.template.spec.nodeSelector.zone == "us-west1-a"`),
						jq.Match(`.spec.template.spec.tolerations[0].key == "nvidia.com/gpu"`),
						jq.Match(`.spec.template.spec.tolerations[0].operator == "Exists"`),
						jq.Match(`.spec.template.spec.tolerations[0].effect == "NoSchedule"`),
					),
				}),
		},
		{
			name: "allows workload without hardware profile annotation",
			testFn: testHardwareProfileWorkload(
				HardwareProfileTestCase{
					name:              "no-annotation",
					workloadName:      "notebook-no-annotation",
					profileName:       "",    // No profile name
					profileSpec:       nil,   // No hardware profile spec
					shouldBlock:       false, // Should allow
					expectedError:     "",
					errorMsg:          "Expected notebook without hardware profile annotation to be allowed",
					expectedCondition: nil,
				}),
		},
	}

	// Run the test cases
	RunTestCases(t, testCases)
}

// setupNamespace creates a test namespace with the provided labels.
// This function merges any additional labels provided.
// Note: The base "test-type: kqueue" label is no longer automatically added - it must be explicitly provided if needed.
//
// Parameters:
//   - namespaceName: The name of the namespace to create
//   - labels: Optional label maps to merge with the base labels.
func (tc *KueueTestCtx) setupNamespace(namespaceName string, labels ...map[string]string) {
	// Labels
	namespaceLabels := make(map[string]string)

	// Merge additional labels if provided
	for _, labelMap := range labels {
		for key, value := range labelMap {
			namespaceLabels[key] = value
		}
	}

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(CreateNamespaceWithLabels(namespaceName, namespaceLabels)),
		WithCustomErrorMsg("Failed to create test namespace '%s'", namespaceName),
	)
}

// ensureClusterAndLocalQueueExist validates that both the default ClusterQueue
// and LocalQueue resources exist in the cluster with proper configuration.
//
// Parameters:
//   - localQueueNamespaceName: The LocalQueue namespaced name
func (tc *KueueTestCtx) ensureClusterAndLocalQueueExist(localQueueNamespaceName string) {
	// Validate that ClusterQueue exists with proper namespace selector and resource groups
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ClusterQueue, types.NamespacedName{Name: kueueDefaultClusterQueueName, Namespace: metav1.NamespaceAll}),
		WithCondition(And(
			// Validate namespace selector is properly configured for Kueue-managed namespaces
			jq.Match(`.spec.namespaceSelector.matchLabels."%s" == "true"`, cluster.KueueManagedLabelKey),
			// Validate at least one resource group exists
			jq.Match(`.spec.resourceGroups | length >= 1`),
		)),
		WithCustomErrorMsg("ClusterQueue should exist with proper namespace selector and resource groups"),
		WithEventuallyTimeout(tc.TestTimeouts.defaultEventuallyTimeout),
	)

	// Validate that LocalQueue exists for the managed namespace
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.LocalQueue, types.NamespacedName{Name: kueueDefaultLocalQueueName, Namespace: localQueueNamespaceName}),
		WithCustomErrorMsg("LocalQueue should exist in namespace '%s'", localQueueNamespaceName),
	)
}

func (tc *KueueTestCtx) ValidateKueueComponentDisabled(t *testing.T) {
	t.Helper()

	// Ensure that DataScienceCluster exists and its component state is "Removed", with the "Ready" condition false.
	tc.UpdateComponentStateInDataScienceCluster(operatorv1.Removed)

	// Ensure that any Deployment resources for the component are not present
	tc.EnsureResourcesGone(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace}),
		WithListOptions(
			&client.ListOptions{
				LabelSelector: k8slabels.Set{
					labels.PlatformPartOf: strings.ToLower(tc.GVK.Kind),
				}.AsSelector(),
			},
		),
		WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
	)

	// Ensure that the resources associated with the component do not exist
	tc.EnsureResourcesGone(WithMinimalObject(tc.GVK, tc.NamespacedName))
}

func (tc *KueueTestCtx) createKueueConfigMap(t *testing.T) {
	t.Helper()

	kueueConfigMapYAML := `apiVersion: config.kueue.x-k8s.io/v1beta1
kind: Configuration
integrations:
  frameworks:
  - "batch/job"
  - "kubeflow.org/mpijob"
  - "ray.io/rayjob"
  - "ray.io/raycluster"
  - "jobset.x-k8s.io/jobset"
  - "kubeflow.org/paddlejob"
  - "kubeflow.org/pytorchjob"
  - "kubeflow.org/tfjob"
  - "kubeflow.org/xgboostjob"
  - "workload.codeflare.dev/appwrapper"
  - "pod"
  - "deployment"
  - "statefulset"
  - "leaderworkerset.x-k8s.io/leaderworkerset"
`

	kueueConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      kueue.KueueConfigMapName,
			Namespace: tc.AppsNamespace,
		},
		Data: map[string]string{
			kueue.KueueConfigMapEntry: kueueConfigMapYAML,
		},
	}

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(kueueConfigMap),
		WithCustomErrorMsg("Failed to create Kueue ConfigMap '%s'", kueue.KueueConfigMapName),
	)
}
