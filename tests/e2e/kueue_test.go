package e2e_test

import (
	"strings"
	"testing"

	gTypes "github.com/onsi/gomega/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/kueue"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

// degradedConditionTestCase defines a test case for external operator degraded condition monitoring.
// Used by component tests that validate condition propagation from external operators.
type degradedConditionTestCase struct {
	name            string
	conditionType   string
	conditionStatus metav1.ConditionStatus
}

type KueueTestCtx struct {
	*ComponentTestCtx
}

const (
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

	// kueueOperatorDeploymentNN is the NamespacedName for the OCP Kueue operator deployment.
	// We scale this down during condition injection to prevent it from resetting conditions.
	// This is the operator that writes to the Kueue CR status - not the workload controller.
	kueueOperatorDeploymentNN = types.NamespacedName{
		Name:      "openshift-kueue-operator",
		Namespace: kueueOcpOperatorNamespace,
	}
)

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
		{"Validate external operator degraded condition monitoring", componentCtx.ValidateExternalOperatorDegradedMonitoring},
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

	t.Logf("Setting Kueue component (%s) to Removed mode to start with a clean state.", componentApi.KueueInstanceName)
	// since the test may be executed on a non-clean state, let clean it up
	// so first set the component as removed ...
	tc.UpdateComponentStateInDataScienceCluster(operatorv1.Removed)

	t.Logf("Cleaning up any existing Kueue test resources.")
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

	t.Logf("Updating DSC %s to set Kueue managementState to Unmanaged.", tc.DataScienceClusterNamespacedName.Name)
	t.Logf("Verifying Kueue component stays NotReady (Ready=False) because OCP Kueue Operator is missing.")
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

	t.Logf("Setting Kueue component (%s) to Removed mode to start with a clean state.", componentApi.KueueInstanceName)
	// since the test may be executed on a non-clean state, let clean it up
	// so first set the component as removed ...
	tc.UpdateComponentStateInDataScienceCluster(operatorv1.Removed)

	t.Logf("Cleaning up any existing Kueue test resources.")
	// ... and then cleanup tests resources
	cleanupKueueTestResources(t, tc.TestContext)

	t.Logf("Creating test namespace (%s) with Kueue management annotation.", kueueTestManagedNamespace)
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

	t.Logf("Creating Kueue ConfigMap (%s) to verify default Kueue resource is created correctly.", kueue.KueueConfigMapName)
	// Create Kueue ConfigMap to verify default Kueue resource is created correctly
	tc.createKueueConfigMap(t)

	t.Logf("Updating the management state of the component in the DataScienceCluster to Unmanaged (expecting NotReady initially due to missing operator).")
	// Update the management state of the component in the DataScienceCluster.
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, componentName, stateUnmanaged)),
		WithCondition(And(conditionsUnmanagedNotReady...)),
	)

	t.Logf("Installing OCP Kueue Operator (%s/%s) to enable Kueue component readiness.", kueueOcpOperatorNamespace, kueueOpName)
	// Install ocp kueue-operator
	tc.EnsureOperatorInstalledWithGlobalOperatorGroupAndChannel(types.NamespacedName{Name: kueueOpName, Namespace: kueueOcpOperatorNamespace}, kueueOcpOperatorChannel)

	conditionsUnmanagedReady := []gTypes.GomegaMatcher{
		// Validate that the component's management state is updated correctly
		jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, stateUnmanaged),
		// Validate the "Ready" condition for the component
		jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionTrue),
	}

	t.Logf("Verifying the component in the DataScienceCluster transitions to Ready state.")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(And(conditionsUnmanagedReady...)),
	)

	t.Logf("Verifying Kueue configuration (%s) is created with all expected frameworks.", kueue.KueueCRName)
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

	t.Logf("Verifying default ClusterQueue and LocalQueue resources are created in namespace %s.", kueueTestManagedNamespace)
	// Default resources should be created
	tc.ensureClusterAndLocalQueueExist(kueueTestManagedNamespace)

	t.Logf("Verifying default resource flavor (%s) is created.", kueue.DefaultFlavorName)
	// Validate that default resource flavor is created
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ResourceFlavor, types.NamespacedName{Name: kueue.DefaultFlavorName}),
	)

	t.Logf("Deleting Kueue ConfigMap (%s) to clean up test resources.", kueue.KueueConfigMapName)
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

	t.Logf("Setting Kueue component (%s) to Removed mode to start with a clean state.", componentApi.KueueInstanceName)
	// since the test may be executed on a non-clean state, let clean it up
	// so first set the component as removed ...
	tc.UpdateComponentStateInDataScienceCluster(operatorv1.Removed)

	t.Logf("Cleaning up any existing Kueue test resources.")
	// ... and then cleanup tests resources
	cleanupKueueTestResources(t, tc.TestContext)

	t.Logf("Installing OCP Kueue Operator (%s/%s) before testing Unmanaged state.", kueueOcpOperatorNamespace, kueueOpName)
	// UNMANAGED
	// Install ocp kueue-operator
	tc.EnsureOperatorInstalledWithGlobalOperatorGroupAndChannel(types.NamespacedName{Name: kueueOpName, Namespace: kueueOcpOperatorNamespace}, kueueOcpOperatorChannel)

	t.Logf("Defining expected conditions for Unmanaged state (Ready=True).")
	stateUnmanaged := operatorv1.Unmanaged
	conditionsUnmanaged := []gTypes.GomegaMatcher{
		// Validate that the component's management state is updated correctly
		jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, stateUnmanaged),
		// Validate the "Ready" condition for the component
		jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionTrue),
	}

	t.Logf("Updating DSC %s to set Kueue managementState to Unmanaged.", tc.DataScienceClusterNamespacedName.Name)
	// Update the management state of the component in the DataScienceCluster.
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, componentName, stateUnmanaged)),
		WithCondition(And(conditionsUnmanaged...)),
	)

	t.Logf("Creating test namespace (%s) with Kueue management annotation.", kueueTestManagedNamespace)
	// Create a test namespace with Kueue management annotation
	tc.setupNamespace(kueueTestManagedNamespace, KueueManagedLabels)

	t.Logf("Verifying default Kueue resources exist in namespace %s.", kueueTestManagedNamespace)
	// Default resources should be created
	tc.ensureClusterAndLocalQueueExist(kueueTestManagedNamespace)

	t.Logf("Verifying default resource flavor (%s) is created.", kueue.DefaultFlavorName)
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

	t.Logf("Updating DSC %s to set Kueue managementState to Removed.", tc.DataScienceClusterNamespacedName.Name)
	// Update the management state of the component in the DataScienceCluster to Removed.
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, componentName, stateRemoved)),
		WithCondition(And(conditionsRemovedReady...)),
	)

	t.Logf("Verifying default resources are still present in namespace %s.", kueueTestManagedNamespace)
	// Validate default resources are still there
	tc.ensureClusterAndLocalQueueExist(kueueTestManagedNamespace)

	t.Logf("Cleaning up Kueue test resources.")
	// Remove Kueue test resources
	cleanupKueueTestResources(t, tc.TestContext)
}

// ValidateWebhookValidations runs both Kueue and hardware profile webhook validation tests
// with proper Workbenches component setup/teardown.
func (tc *KueueTestCtx) ValidateWebhookValidations(t *testing.T) {
	t.Helper()

	t.Logf("Setting Workbenches component to Managed to install Notebook CRD for webhook tests.")
	// Enable Workbenches component to ensure Notebook CRD is available for webhook tests
	// This is required because webhook tests use Notebook objects which need the Notebook CRD
	// installed by the Workbenches component
	tc.UpdateComponentStateInDataScienceClusterWithKind(operatorv1.Managed, componentApi.WorkbenchesKind)

	t.Logf("Running Kueue and Hardware Profile webhook validation subtests.")
	// Run webhook validation tests as subtests
	t.Run("Kueue webhook validation", tc.ValidateKueueWebhookValidation)
	t.Run("Hardware profile webhook validation", tc.ValidateHardwareProfileWebhookValidation)

	t.Logf("Setting Workbenches component to Removed to cleanup Notebook CRD.")
	// Ensure Workbenches is disabled after tests, even if they fail
	tc.UpdateComponentStateInDataScienceClusterWithKind(operatorv1.Removed, componentApi.WorkbenchesKind)

	t.Logf("Cleaning up Kueue test resources.")
	// Remove Kueue test resources
	cleanupKueueTestResources(t, tc.TestContext)
}

// ValidateKueueWebhookValidation validates the Kueue validating webhook behavior using table-driven tests.
func (tc *KueueTestCtx) ValidateKueueWebhookValidation(t *testing.T) {
	t.Helper()

	t.Logf("Installing OCP Kueue Operator (%s/%s) to enable webhook validation.", kueueOcpOperatorNamespace, kueueOpName)
	// Install ocp kueue-operator
	tc.EnsureOperatorInstalledWithGlobalOperatorGroupAndChannel(types.NamespacedName{Name: kueueOpName, Namespace: kueueOcpOperatorNamespace}, kueueOcpOperatorChannel)

	t.Logf("Setting Kueue component to Unmanaged state.")
	// Ensure Kueue is in Unmanaged state
	tc.UpdateComponentStateInDataScienceCluster(operatorv1.Unmanaged)

	t.Logf("Creating managed test namespace (%s) with Kueue management annotation.", kueueTestManagedNamespace)
	// Ensure the managed namespace exists
	tc.setupNamespace(kueueTestManagedNamespace, KueueManagedLabels)

	t.Logf("Creating non-managed test namespace (%s) for webhook exclusion testing.", kueueTestWebhookNonManagedNamespace)
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

			t.Logf("Creating test Notebook (%s) in namespace (%s).", name, namespace)
			// Create notebook with labels if provided
			notebook := envtestutil.NewNotebook(name, namespace, envtestutil.WithLabels(labels))

			// Handle blocking case first (exceptional path)
			if shouldBlock {
				t.Logf("Verifying webhook blocks creation of invalid Notebook (expected error: %s).", expectedError)
				tc.EnsureWebhookBlocksResourceCreation(
					WithObjectToCreate(notebook),
					WithInvalidValue(expectedError),
					WithCustomErrorMsg(errorMsg),
				)
				return
			}

			// Happy path - webhook allows the resource
			t.Logf("Verifying webhook allows creation of valid Notebook.")
			tc.EventuallyResourceCreatedOrUpdated(
				WithObjectToCreate(notebook),
				WithCustomErrorMsg(errorMsg),
			)
		}
	}

	t.Logf("Defining and running Kueue webhook validation test cases.")
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

	t.Logf("Executing %d webhook validation test cases.", len(testCases))
	// Run the test cases
	RunTestCases(t, testCases)
}

// ValidateHardwareProfileWebhookValidation validates the hardware profile webhook behavior using table-driven tests.
func (tc *KueueTestCtx) ValidateHardwareProfileWebhookValidation(t *testing.T) {
	t.Helper()

	t.Logf("Creating non-managed namespace (%s) for hardware profile testing.", kueueTestHardwareProfileNamespace)
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

			t.Logf("Running HardwareProfile test case: %s", testCase.name)
			// Use the dedicated hardware profile test namespace (non-managed to avoid Kueue validation)
			testNamespace := kueueTestHardwareProfileNamespace

			// Create hardware profile if spec is provided
			if testCase.profileSpec != nil {
				t.Logf("Creating HardwareProfile %s in namespace %s.", testCase.profileName, testNamespace)
				hwp := envtestutil.NewHardwareProfile(testCase.profileName, testNamespace, envtestutil.WithHardwareProfileSpec(*testCase.profileSpec))
				tc.EventuallyResourceCreatedOrUpdated(
					WithObjectToCreate(hwp),
					WithCustomErrorMsg("Failed to create hardware profile for %s", testCase.name),
				)
			}

			t.Logf("Creating Notebook workload %s referencing profile %s.", testCase.workloadName, testCase.profileName)
			// Create notebook workload
			notebook := envtestutil.NewNotebook(testCase.workloadName, testNamespace, envtestutil.WithHardwareProfile(testCase.profileName))

			// Handle blocking case first (exceptional path)
			if testCase.shouldBlock {
				t.Logf("Verifying webhook blocks creation of invalid Notebook (expected error: %s).", testCase.expectedError)
				tc.EnsureWebhookBlocksResourceCreation(
					WithObjectToCreate(notebook),
					WithInvalidValue(testCase.expectedError),
					WithCustomErrorMsg(testCase.errorMsg),
				)
				return
			}

			t.Logf("Verifying webhook allows creation of valid Notebook and resources are injected/scheduled correctly.")
			// Happy path - webhook allows the resource
			tc.EventuallyResourceCreatedOrUpdated(
				WithObjectToCreate(notebook),
				WithCustomErrorMsg(testCase.errorMsg),
				WithCondition(testCase.expectedCondition),
			)
		}
	}

	t.Logf("Defining and running HardwareProfile webhook validation test cases.")
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

	t.Logf("Executing %d HardwareProfile test cases.", len(testCases))
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

	t.Logf("Setting DSC component %s to Removed state.", componentApi.KueueInstanceName)
	// Ensure that DataScienceCluster exists and its component state is "Removed", with the "Ready" condition false.
	tc.UpdateComponentStateInDataScienceCluster(operatorv1.Removed)

	t.Logf("Verifying no Deployments remain in namespace %s with part-of=%s label.", tc.AppsNamespace, strings.ToLower(tc.GVK.Kind))
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

	t.Logf("Ensuring the component CR %s/%s is removed.", tc.NamespacedName.Namespace, tc.NamespacedName.Name)
	// Ensure that the resources associated with the component do not exist
	tc.EnsureResourcesGone(WithMinimalObject(tc.GVK, tc.NamespacedName))
}

func (tc *KueueTestCtx) createKueueConfigMap(t *testing.T) {
	t.Helper()

	t.Logf("Creating Kueue ConfigMap (%s) in namespace (%s).", kueue.KueueConfigMapName, tc.AppsNamespace)

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

// ValidateExternalOperatorDegradedMonitoring ensures that when the external Kueue operator CR
// has degraded conditions, they properly propagate to both the component CR and the DSC CR,
// and that recovery is properly reflected as well.
//
// Validates the full condition propagation chain:
// External Operator CR > Kueue Component CR > DataScienceCluster CR
//
// Keep the real Kueue operator installed but scale its deployment to 0 during
// condition injection. This way OperatorExists() still passes (OLM's OperatorCondition exists),
// but the operator can't reset conditions we inject.
func (tc *KueueTestCtx) ValidateExternalOperatorDegradedMonitoring(t *testing.T) {
	t.Helper()

	// condition types monitored by the Kueue Component
	testCases := []degradedConditionTestCase{
		{
			name:            "Degraded=True triggers component degradation",
			conditionType:   "Degraded",
			conditionStatus: metav1.ConditionTrue,
		},
		{
			name:            "Available=False triggers component degradation",
			conditionType:   "Available",
			conditionStatus: metav1.ConditionFalse,
		},
		{
			name:            "CertManagerAvailable=False triggers component degradation",
			conditionType:   "CertManagerAvailable",
			conditionStatus: metav1.ConditionFalse,
		},
	}

	t.Log("Resetting environment for external operator monitoring test (Component=Removed).")
	tc.UpdateComponentStateInDataScienceCluster(operatorv1.Removed)
	cleanupKueueTestResources(t, tc.TestContext)

	t.Logf("Ensuring Kueue OCP operator is installed (namespace=%s, name=%s).", kueueOcpOperatorNamespace, kueueOpName)
	tc.EnsureOperatorInstalledWithGlobalOperatorGroupAndChannel(
		types.NamespacedName{Name: kueueOpName, Namespace: kueueOcpOperatorNamespace},
		kueueOcpOperatorChannel,
	)

	t.Logf("Creating managed test namespace with Kueue management annotation (namespace=%s).", kueueTestManagedNamespace)
	tc.setupNamespace(kueueTestManagedNamespace, KueueManagedLabels)

	t.Logf("Creating Kueue ConfigMap required for component reconciliation (namespace=%s, name=%s).", tc.AppsNamespace, kueue.KueueConfigMapName)
	tc.createKueueConfigMap(t)

	t.Logf("Enabling Kueue component by setting to Unmanaged mode (namespace=%s, name=%s).", tc.AppsNamespace, componentApi.KueueInstanceName)
	tc.UpdateComponentStateInDataScienceCluster(operatorv1.Unmanaged)

	// Kueue operator auto-creates its CR, so the component should be healthy initially
	t.Logf("Verifying Kueue component is initially healthy (DependenciesAvailable=True) (namespace=%s, name=%s).", tc.AppsNamespace, componentApi.KueueInstanceName)
	kueueNN := types.NamespacedName{Name: componentApi.KueueInstanceName}
	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, kueueNN),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionDependenciesAvailable, metav1.ConditionTrue),
		),
	)

	t.Logf("Verifying DSC is healthy (KueueReady=True) before degradation tests (namespace=%s, name=%s).",
		tc.DataScienceClusterNamespacedName.Namespace, tc.DataScienceClusterNamespacedName.Name)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionTrue),
		),
	)

	t.Logf("Scaling down Kueue operator deployment to prevent condition reset (namespace=%s, name=%s).", kueueOcpOperatorNamespace, kueueOperatorDeploymentNN.Name)
	originalReplicas := tc.scaleKueueOperator(t, 0)

	// Run each test case (inject condition, verify, clear condition, verify recovery)
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			tc.runDegradedConditionTestCase(t, testCase)
		})
	}

	t.Logf("Scaling Kueue operator deployment back up (namespace=%s, name=%s).", kueueOcpOperatorNamespace, kueueOperatorDeploymentNN.Name)
	tc.scaleKueueOperator(t, originalReplicas)

	t.Log("All external operator degraded condition monitoring tests passed successfully.")
}

// ensureKueueBaseline clears Kueue operator conditions, asserts component/DSC healthy.
// Returns the Kueue CR for use in test assertions.
func (tc *KueueTestCtx) ensureKueueBaseline(t *testing.T) *unstructured.Unstructured {
	t.Helper()

	kueueNN := types.NamespacedName{Name: componentApi.KueueInstanceName}
	kueueCR := tc.FetchSingleResourceOfKind(gvk.KueueConfigV1, "")

	tc.ClearAllConditionsFromResourceStatus(kueueCR)
	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, kueueNN),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionDependenciesAvailable, metav1.ConditionTrue)),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue)),
	)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionTrue)),
	)

	return kueueCR
}

// scaleKueueOperator scales the Kueue operator deployment by patching the CSV.
// blocks until the deployment reaches the target replica count.
func (tc *KueueTestCtx) scaleKueueOperator(t *testing.T, replicas int32) int32 {
	t.Helper()

	t.Logf("Scaling Kueue operator via CSV in namespace %s to %d replicas.", kueueOcpOperatorNamespace, replicas)
	originalReplicas := tc.ScaleCSVDeploymentReplicas(
		kueueOcpOperatorNamespace,
		"kueue",
		kueueOperatorDeploymentNN.Name,
		replicas,
	)
	t.Logf("Kueue operator deployment scaled to %d replicas in namespace %s.", replicas, kueueOcpOperatorNamespace)
	return originalReplicas
}

// runDegradedConditionTestCase runs a single degraded condition test case.
// It injects a condition into the Kueue operator CR, verifies propagation to the component CR and DSC,
// then clears the condition and verifies recovery.
func (tc *KueueTestCtx) runDegradedConditionTestCase(t *testing.T, testCase degradedConditionTestCase) {
	t.Helper()

	t.Logf("Running test case: %s (Condition: %s=%s)", testCase.name, testCase.conditionType, testCase.conditionStatus)

	kueueNN := types.NamespacedName{Name: componentApi.KueueInstanceName}

	// Establish baseline (clears conditions, asserts healthy)
	kueueCR := tc.ensureKueueBaseline(t)

	t.Logf("Simulating external operator degradation: Injecting %s=%s into operator CR.", testCase.conditionType, testCase.conditionStatus)
	tc.InjectConditionIntoResourceStatus(
		kueueCR,
		testCase.conditionType,
		testCase.conditionStatus,
		"TestInjected",
		"Simulated condition for e2e test: "+testCase.conditionType+"="+string(testCase.conditionStatus),
	)

	t.Logf("Verifying Kueue component CR (%s) reacts by setting DependenciesAvailable=False and Ready=False.", kueueNN)
	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, kueueNN),
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

	t.Logf("Verifying DSC CR (%s) reflects the component's degraded state (KueueReady=False).", tc.DataScienceClusterNamespacedName)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionFalse),
		),
	)

	t.Logf("Clearing injected condition %s from operator CR to test recovery.", testCase.conditionType)
	kueueCR = tc.FetchSingleResourceOfKind(gvk.KueueConfigV1, "")
	tc.RemoveConditionFromResourceStatus(kueueCR, testCase.conditionType)

	t.Logf("Verifying Kueue component CR (%s) recovers (DependenciesAvailable=True, Ready=True).", kueueNN)
	tc.EnsureResourceExists(
		WithMinimalObject(tc.GVK, kueueNN),
		WithCondition(
			And(
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionDependenciesAvailable, metav1.ConditionTrue),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
			),
		),
	)

	t.Logf("Verifying DSC CR (%s) recovers (KueueReady=True).", tc.DataScienceClusterNamespacedName)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionTrue),
		),
	)

	t.Logf("Test case passed: %s", testCase.name)
}
