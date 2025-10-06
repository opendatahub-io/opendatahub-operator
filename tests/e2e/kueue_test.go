package e2e_test

import (
	"strconv"
	"strings"
	"testing"

	gTypes "github.com/onsi/gomega/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/kueue"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/mocks"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const (
	kueueOcpOperatorNamespace           = "openshift-kueue-operator" // Namespace for the Kueue Operator
	kueueOcpOperatorChannel             = "stable-v0.2"
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
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate CRDs reinstated", componentCtx.ValidateCRDReinstated},
		{"Validate pre check", componentCtx.ValidateKueuePreCheck},
		{"Validate component releases", componentCtx.ValidateComponentReleases},
	}

	// Only add OCP Kueue operator test if OCP version is 4.18 or above
	meets, err := componentCtx.CheckMinOCPVersion("4.18.0")
	componentCtx.g.Expect(err).ShouldNot(HaveOccurred(), "Failed to check OCP version")
	if meets {
		testCases = append(testCases,
			TestCase{"Validate component managed error with ocp kueue-operator installed", componentCtx.ValidateKueueManagedWithOcpKueueOperator},
			TestCase{"Validate component unmanaged error with ocp kueue-operator not installed", componentCtx.ValidateKueueUnmanagedWithoutOcpKueueOperator},
			TestCase{"Validate component managed to unmanaged transition", componentCtx.ValidateKueueManagedToUnmanagedTransition},
			TestCase{"Validate component managed to removed to unmanaged transition with config migration", componentCtx.ValidateKueueManagedToRemovedToUnmanagedTransition(true)},
			TestCase{"Validate component managed to removed to unmanaged transition without config migration", componentCtx.ValidateKueueManagedToRemovedToUnmanagedTransition(false)},
			TestCase{"Validate component unmanaged to managed transition", componentCtx.ValidateKueueUnmanagedToManagedTransition},
		)
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
		TestCase{"Validate component disabled", componentCtx.ValidateComponentDisabled})

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateCRDReinstated ensures that required CRDs are reinstated if deleted.
func (tc *KueueTestCtx) ValidateCRDReinstated(t *testing.T) {
	t.Helper()

	crds := []CRD{
		{Name: "workloads.kueue.x-k8s.io", Version: "v1beta1"},
		{Name: "multikueueclusters.kueue.x-k8s.io", Version: "v1beta1"},
		{Name: "multikueueconfigs.kueue.x-k8s.io", Version: "v1beta1"},
	}

	tc.ValidateCRDsReinstated(t, crds)
}

// ValidateKueuePreCheck performs a pre-check by manipulating CRDs and validating expected behavior.
func (tc *KueueTestCtx) ValidateKueuePreCheck(t *testing.T) {
	t.Helper()

	var mkConfig = "multikueueconfigs.kueue.x-k8s.io"
	var mkCluster = "multikueueclusters.kueue.x-k8s.io"

	// Ensure DataScienceCluster component is initially removed.
	tc.UpdateComponentStateInDataScienceCluster(operatorv1.Removed)

	// Verify there are no instances of the component
	tc.EnsureResourceGone(WithMinimalObject(tc.GVK, types.NamespacedName{Name: componentApi.KueueInstanceName}))

	// Delete and validate CRDs
	tc.deleteAndValidateCRD(mkCluster)
	tc.deleteAndValidateCRD(mkConfig)

	// Create new CRDs
	tc.createMockCRD(gvk.MultikueueClusterV1Alpha1, "kueue")
	tc.createMockCRD(gvk.MultiKueueConfigV1Alpha1, "kueue")

	// Update DataScienceCluster to Managed state and check readiness condition
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, strings.ToLower(tc.GVK.Kind), operatorv1.Managed)),
		WithCondition(
			And(
				jq.Match(`.spec.components.%s.managementState == "%s"`, strings.ToLower(tc.GVK.Kind), operatorv1.Managed),
				jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionFalse),
			),
		),
	)

	// Delete the CRDs.
	tc.DeleteResource(
		WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: mkCluster}),
		WithForegroundDeletion(),
	)
	tc.DeleteResource(
		WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: mkConfig}),
		WithForegroundDeletion(),
	)

	// Verify the DataScienceCluster become "Ready"
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionTrue)),
	)
}

// ValidateComponentEnabled ensures that if the component is in Managed state and ocp kueue operator is installed, then its status is "Not Ready".
func (tc *KueueTestCtx) ValidateKueueManagedWithOcpKueueOperator(t *testing.T) {
	t.Helper()

	componentName := strings.ToLower(tc.GVK.Kind)

	// since the test may be executed on a non-clean state, let clean it up
	// so first set the component as removed ...
	tc.UpdateComponentStateInDataScienceCluster(operatorv1.Removed)

	// ... and then cleanup tests resources
	cleanupKueueTestResources(t, tc.TestContext)

	namespacedName := types.NamespacedName{Name: kueueOpName, Namespace: kueueOcpOperatorNamespace}
	// Install ocp kueue-operator
	tc.EnsureOperatorInstalledWithChannel(namespacedName, false, kueueOcpOperatorChannel)

	state := operatorv1.Managed

	// State must be Managed, Ready condition must be false because ocp kueue-operator is installed
	conditions := []gTypes.GomegaMatcher{
		// Validate that the component's management state is updated correctly
		jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, state),

		// Validate the "Ready" condition for the component
		jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionFalse),
	}

	tc.ConsistentlyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, componentName, state)),
		WithCondition(And(conditions...)),
	)

	// Due to the conflict with OCP Kueue operator, default Kueue resources should NOT be created
	// Validate that ClusterQueue does not exist
	tc.EnsureResourceDoesNotExist(
		WithMinimalObject(gvk.ClusterQueue, types.NamespacedName{Name: kueueDefaultClusterQueueName}),
	)

	// Validate that Kueue configuration does not exist
	tc.EnsureResourceDoesNotExist(
		WithMinimalObject(gvk.KueueConfigV1, types.NamespacedName{Name: kueue.KueueCRName}),
	)
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

// ValidateComponentEnabled ensures the transition between Managed and Unmanaged state happens as expected.
func (tc *KueueTestCtx) ValidateKueueManagedToUnmanagedTransition(t *testing.T) {
	t.Helper()

	componentName := strings.ToLower(tc.GVK.Kind)

	// since the test may be executed on a non-clean state, let clean it up
	// so first set the component as removed ...
	tc.UpdateComponentStateInDataScienceCluster(operatorv1.Removed)

	// ... and then cleanup tests resources
	cleanupKueueTestResources(t, tc.TestContext)

	// Create a test namespace with Kueue management annotation
	tc.setupNamespace(kueueTestManagedNamespace, KueueManagedLabels)

	// MANAGED
	stateManaged := operatorv1.Managed
	conditionsManagedReady := []gTypes.GomegaMatcher{
		// Validate that the component's management state is updated correctly
		jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, stateManaged),
		// Validate the "Ready" condition for the component
		jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionTrue),
	}

	// Update the management state of the component in the DataScienceCluster.
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, componentName, stateManaged)),
		WithCondition(And(conditionsManagedReady...)),
	)

	// During Managed state, validate that default Kueue resources are created
	// Validate that ClusterQueue exists
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ClusterQueue, types.NamespacedName{Name: kueueDefaultClusterQueueName, Namespace: metav1.NamespaceAll}),
	)

	// Validate that LocalQueue exists for the managed namespace
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.LocalQueue, types.NamespacedName{Name: kueueDefaultLocalQueueName, Namespace: kueueTestManagedNamespace}),
	)

	// before changing the embedded Kueue management state, ensure the related configuration
	// ConfigMap is left on the cluster, so it can be taken into account to create the default
	// Kueue CR for the OpenShift Kueue Operator
	tc.setManagedAnnotation(
		gvk.ConfigMap,
		types.NamespacedName{Name: kueue.KueueConfigMapName, Namespace: tc.AppsNamespace},
		false,
	)

	// UNMANAGED
	stateUnmanaged := operatorv1.Unmanaged
	conditionsUnmanagedNotReady := []gTypes.GomegaMatcher{
		// Validate that the component's management state is updated correctly
		jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, stateUnmanaged),

		// Validate the "Ready" condition for the component
		jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionFalse),
	}

	// Update the management state of the component in the DataScienceCluster.
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, componentName, stateUnmanaged)),
		WithCondition(And(conditionsUnmanagedNotReady...)),
	)

	// Validate that Kueue's ConfigMap still exists
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ConfigMap, types.NamespacedName{Name: kueue.KueueConfigMapName, Namespace: tc.AppsNamespace}),
	)

	// Ensure embedded kueue operator is not running
	tc.EnsureResourceDoesNotExist(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace}),
		WithListOptions(
			&client.ListOptions{
				Namespace: tc.AppsNamespace,
				LabelSelector: k8slabels.Set{
					labels.PlatformPartOf: strings.ToLower(tc.GVK.Kind),
				}.AsSelector(),
			},
		),
	)

	// During Unmanaged state, resources should still exist since our action creates them for both states
	// Validate that ClusterQueue still exists
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ClusterQueue, types.NamespacedName{Name: kueueDefaultClusterQueueName, Namespace: metav1.NamespaceAll}),
	)

	// Validate that LocalQueue still exists for the managed namespace
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.LocalQueue, types.NamespacedName{Name: kueueDefaultLocalQueueName, Namespace: kueueTestManagedNamespace}),
	)

	// Install ocp kueue-operator
	tc.EnsureOperatorInstalledWithChannel(types.NamespacedName{Name: kueueOpName, Namespace: kueueOcpOperatorNamespace}, false, kueueOcpOperatorChannel)

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

	// Validate that Kueue configuration is created
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.KueueConfigV1, types.NamespacedName{Name: kueue.KueueCRName}),
		// check that the Kueue CR contains information that are not set by default, but
		// can only be taken from the embedded Kueue ConfigMap
		WithCondition(jq.Match(`.spec.config.integrations.frameworks | contains(["XGBoostJob"])`)),
	)
}

// ValidateKueueManagedToRemovedToUnmanagedTransition ensures the transition from Managed to Removed and then to Unmanaged state happens as expected.
func (tc *KueueTestCtx) ValidateKueueManagedToRemovedToUnmanagedTransition(migrateConfig bool) func(t *testing.T) {
	return func(t *testing.T) {
		t.Helper()

		componentName := strings.ToLower(tc.GVK.Kind)

		// since the test may be executed on a non-clean state, let clean it up
		// so first set the component as removed ...
		tc.UpdateComponentStateInDataScienceCluster(operatorv1.Removed)

		// ... and then cleanup tests resources
		cleanupKueueTestResources(t, tc.TestContext)

		// Create a test namespace with Kueue legacy management annotation
		tc.setupNamespace(kueueTestLegacyManagedNamespace, KueueLegacyManagedLabels)

		// MANAGED
		stateManaged := operatorv1.Managed
		conditionsManaged := []gTypes.GomegaMatcher{
			// Validate that the component's management state is updated correctly
			jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, stateManaged),
			// Validate the "Ready" condition for the component
			jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionTrue),
		}

		// Update the management state of the component in the DataScienceCluster to Managed.
		tc.EventuallyResourcePatched(
			WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
			WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, componentName, stateManaged)),
			WithCondition(And(conditionsManaged...)),
		)

		// During Managed state, validate that default Kueue resources are created
		tc.ensureClusterAndLocalQueueExist(kueueTestLegacyManagedNamespace)

		if migrateConfig {
			// before changing the embedded Kueue management state, ensure the related configuration
			// ConfigMap is left on the cluster, so it can be taken into account to create the default
			// Kueue CR for the OpenShift Kueue Operator
			tc.setManagedAnnotation(
				gvk.ConfigMap,
				types.NamespacedName{Name: kueue.KueueConfigMapName, Namespace: tc.AppsNamespace},
				false,
			)
		}

		// REMOVED
		stateRemoved := operatorv1.Removed
		conditionsRemoved := []gTypes.GomegaMatcher{
			// Validate that the component's management state is updated correctly
			jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, stateRemoved),
			// Validate the "Ready" condition for the component
			jq.Match(`.status.conditions[] | select(.type == "%sReady") | .reason == "%s"`, tc.GVK.Kind, stateRemoved),
		}

		// Update the management state of the component in the DataScienceCluster to Removed.
		tc.EventuallyResourcePatched(
			WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
			WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, componentName, stateRemoved)),
			WithCondition(And(conditionsRemoved...)),
		)

		// During Removed state, validate that default Kueue resources are left untouched.
		tc.ensureClusterAndLocalQueueExist(kueueTestLegacyManagedNamespace)

		if migrateConfig {
			// Validate that Kueue's ConfigMap still exists
			tc.EnsureResourcesExist(
				WithMinimalObject(gvk.ConfigMap, types.NamespacedName{Name: kueue.KueueConfigMapName, Namespace: tc.AppsNamespace}),
			)
		} else {
			// Validate that Kueue's ConfigMap is gone
			tc.EnsureResourceGone(
				WithMinimalObject(gvk.ConfigMap, types.NamespacedName{Name: kueue.KueueConfigMapName, Namespace: tc.AppsNamespace}),
			)
		}

		// UNMANAGED
		// Install ocp kueue-operator
		tc.EnsureOperatorInstalledWithChannel(types.NamespacedName{Name: kueueOpName, Namespace: kueueOcpOperatorNamespace}, false, kueueOcpOperatorChannel)
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

		// During Unmanaged state, resources should still exist since our action creates them for both states
		tc.ensureClusterAndLocalQueueExist(kueueTestLegacyManagedNamespace)

		opts := []ResourceOpts{
			WithMinimalObject(gvk.KueueConfigV1, types.NamespacedName{Name: kueue.KueueCRName}),
		}

		if migrateConfig {
			// check that the Kueue CR contains information that are not set by default, but
			// can only be taken from the embedded Kueue ConfigMap
			opts = append(opts, WithCondition(jq.Match(`.spec.config.integrations.frameworks | contains(["XGBoostJob"])`)))
		} else {
			// check that the Kueue CR contains only default information
			opts = append(opts, WithCondition(jq.Match(`.spec.config.integrations.frameworks | contains(["XGBoostJob"]) | not`)))
		}

		// Validate that Kueue configuration is created
		tc.EnsureResourceExists(opts...)
	}
}

// ValidateWebhookValidations runs both Kueue and hardware profile webhook validation tests
// with proper Workbenches component setup/teardown.
func (tc *KueueTestCtx) ValidateWebhookValidations(t *testing.T) {
	t.Helper()

	// Enable Workbenches component to ensure Notebook CRD is available for webhook tests
	// This is required because webhook tests use Notebook objects which need the Notebook CRD
	// installed by the Workbenches component
	tc.UpdateComponentStateInDataScienceClusterWithKind(operatorv1.Managed, "Workbenches")

	// Run webhook validation tests as subtests
	t.Run("Kueue webhook validation", tc.ValidateKueueWebhookValidation)
	t.Run("Hardware profile webhook validation", tc.ValidateHardwareProfileWebhookValidation)

	// Ensure Workbenches is disabled after tests, even if they fail
	tc.UpdateComponentStateInDataScienceClusterWithKind(operatorv1.Removed, "Workbenches")

	// Remove Kueue test resources
	cleanupKueueTestResources(t, tc.TestContext)
}

// ValidateKueueWebhookValidation validates the Kueue validating webhook behavior using table-driven tests.
func (tc *KueueTestCtx) ValidateKueueWebhookValidation(t *testing.T) {
	t.Helper()

	// Ensure Kueue is in Managed state
	tc.UpdateComponentStateInDataScienceCluster(operatorv1.Managed)

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

// ValidateKueueUnmanagedToManagedTransition ensures the transition from Unmanaged to Managed state happens as expected.
func (tc *KueueTestCtx) ValidateKueueUnmanagedToManagedTransition(t *testing.T) {
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
	// Install ocp kueue-operator
	tc.EnsureOperatorInstalledWithChannel(types.NamespacedName{Name: kueueOpName, Namespace: kueueOcpOperatorNamespace}, false, kueueOcpOperatorChannel)
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

	// During Unmanaged state, resources should still exist since our action creates them for both states
	tc.ensureClusterAndLocalQueueExist(kueueTestManagedNamespace)

	// Validate that Kueue configuration is created
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.KueueConfigV1, types.NamespacedName{Name: kueue.KueueCRName}),
	)

	// Validate that default resource flavor is created
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ResourceFlavor, types.NamespacedName{Name: kueue.DefaultFlavorName}),
	)

	// MANAGED
	stateManaged := operatorv1.Managed
	conditionsManagedNotReady := []gTypes.GomegaMatcher{
		// Validate that the component's management state is updated correctly
		jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, stateManaged),
		// Validate the "Ready" condition for the component
		jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionFalse),
	}

	// Update the management state of the component in the DataScienceCluster to Managed.
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, componentName, stateManaged)),
		WithCondition(And(conditionsManagedNotReady...)),
	)

	// Uninstall ocp kueue-operator
	tc.UninstallOperator(types.NamespacedName{Name: kueueOpName, Namespace: kueueOcpOperatorNamespace})

	conditionsManagedReady := []gTypes.GomegaMatcher{
		// Validate that the component's management state is updated correctly
		jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, stateManaged),
		// Validate the "Ready" condition for the component
		jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionTrue),
	}

	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(And(conditionsManagedReady...)),
	)

	// Validate default resources are still there
	tc.ensureClusterAndLocalQueueExist(kueueTestManagedNamespace)

	// Validate that Kueue configuration is still there
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.KueueConfigV1, types.NamespacedName{Name: kueue.KueueCRName}),
	)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ResourceFlavor, types.NamespacedName{Name: kueue.DefaultFlavorName}),
	)

	// Remove Kueue test resources
	cleanupKueueTestResources(t, tc.TestContext)
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
	)

	// Validate that LocalQueue exists for the managed namespace
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.LocalQueue, types.NamespacedName{Name: kueueDefaultLocalQueueName, Namespace: localQueueNamespaceName}),
		WithCustomErrorMsg("LocalQueue should exist in namespace '%s'", localQueueNamespaceName),
	)
}

// deleteAndValidateCRD deletes a specified CRD and ensures it no longer exists in the cluster.
// It uses foreground deletion propagation policy to ensure proper cleanup of dependent resources.
//
// Parameters:
//   - crdName: The name of the Custom Resource Definition to delete
func (tc *KueueTestCtx) deleteAndValidateCRD(crdName string) {
	// Delete the CRD
	tc.DeleteResource(
		WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: crdName}),
		WithForegroundDeletion(),
	)
}

// createMockCRD creates a mock Custom Resource Definition for testing purposes.
// It generates a CRD with the specified GroupVersionKind and associates it with
// the given component name.
//
// Parameters:
//   - gvk: The GroupVersionKind for the mock CRD
//   - componentName: The component name to associate with the CRD
func (tc *KueueTestCtx) createMockCRD(gvk schema.GroupVersionKind, componentName string) {
	crd := mocks.NewMockCRD(gvk.Group, gvk.Version, strings.ToLower(gvk.Kind), componentName)

	tc.EventuallyResourceCreatedOrUpdated(WithObjectToCreate(crd))
}

// setManagedAnnotation updates the managed annotation on a resource and validates
// the change. When managed is true, the resource is marked as managed by the ODH operator
// and should have an owner reference. When false, the annotation is set to false
// and owner references are removed.
//
// Parameters:
//   - gvk: The GroupVersionKind of the resource to update
//   - name: The NamespacedName of the resource to update
//   - managed: Whether the resource should be marked as managed (true) or unmanaged (false)
func (tc *KueueTestCtx) setManagedAnnotation(gvk schema.GroupVersionKind, name types.NamespacedName, managed bool) {
	ownerReferencesCount := 0
	if managed {
		ownerReferencesCount = 1
	}

	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk, name),
		WithMutateFunc(testf.Transform(`.metadata.annotations."%s" = "%s"`, annotations.ManagedByODHOperator, strconv.FormatBool(managed))),
		WithCondition(And(
			jq.Match(`.metadata.annotations."%s" == "%s"`, annotations.ManagedByODHOperator, strconv.FormatBool(managed)),
			jq.Match(`.metadata.ownerReferences | length == %d`, ownerReferencesCount),
		)),
	)
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
