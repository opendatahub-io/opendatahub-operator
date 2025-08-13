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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	infrav1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/kueue"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
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
		{"Validate deployment deletion recovery", componentCtx.ValidateDeploymentDeletionRecovery},
		{"Validate configmap deletion recovery", componentCtx.ValidateConfigMapDeletionRecovery},
		{"Validate service deletion recovery", componentCtx.ValidateServiceDeletionRecovery},
		// TODO: disabled until RHOAIENG-32503 is resolved
		// {"Validate rbac deletion recovery", componentCtx.ValidateRBACDeletionRecovery},
		// {"Validate serviceaccount deletion recovery", componentCtx.ValidateServiceAccountDeletionRecovery},
	}

	// Only add OCP Kueue operator test if OCP version is 4.18 or above
	meets, err := componentCtx.CheckMinOCPVersion("4.18.0")
	componentCtx.g.Expect(err).ShouldNot(HaveOccurred(), "Failed to check OCP version")
	if meets {
		testCases = append(testCases,
			TestCase{"Validate component managed error with ocp kueue-operator installed", componentCtx.ValidateKueueManagedWhitOcpKueueOperator},
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
			// Enable Workbenches component to ensure Notebook CRD is available for webhook tests
			// This is required because webhook tests use Notebook objects which need the Notebook CRD
			// installed by the Workbenches component
			TestCase{"Enable Workbenches component", func(t *testing.T) {
				t.Helper()
				componentCtx.UpdateComponentStateInDataScienceClusterWhitKind(operatorv1.Managed, "Workbenches")
			}},
			TestCase{"Validate Kueue webhook validation", componentCtx.ValidateKueueWebhookValidation},
			TestCase{"Validate hardware profile webhook validation", componentCtx.ValidateHardwareProfileWebhookValidation},
			// Cleanup Workbenches immediately after webhook tests
			TestCase{"Disable Workbenches component", func(t *testing.T) {
				t.Helper()
				componentCtx.UpdateComponentStateInDataScienceClusterWhitKind(operatorv1.Removed, "Workbenches")
			}},
		)
	}

	// Always run component disable test last
	testCases = append(testCases, TestCase{"Validate component disabled", componentCtx.ValidateComponentDisabled})
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
	tc.EventuallyResourceCreatedOrUpdated(
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
	propagationPolicy := metav1.DeletePropagationForeground
	tc.DeleteResource(
		WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: mkCluster}),
		WithClientDeleteOptions(
			&client.DeleteOptions{
				PropagationPolicy: &propagationPolicy,
			}),
	)
	tc.DeleteResource(
		WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: mkConfig}),
		WithClientDeleteOptions(
			&client.DeleteOptions{
				PropagationPolicy: &propagationPolicy,
			}),
	)

	// Verify the DataScienceCluster become "Ready"
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionTrue)),
	)
}

// ValidateComponentEnabled ensures that if the component is in Managed state and ocp kueue operator is installed, then its status is "Not Ready".
func (tc *KueueTestCtx) ValidateKueueManagedWhitOcpKueueOperator(t *testing.T) {
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

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, componentName, state)),
	)

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(And(conditions...)),
	)
	tc.ConsistentlyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
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

	// State must be Managed, Ready condition must be false because ocp kueue-operator is installed
	conditionsNotReady := []gTypes.GomegaMatcher{
		// Validate that the component's management state is updated correctly
		jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, stateUnmanaged),

		// Validate the "Ready" condition for the component
		jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionFalse),
	}

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, componentName, stateUnmanaged)),
		WithCondition(And(conditionsNotReady...)),
	)
	tc.ConsistentlyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
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
	tc.setupKueueManagedNamespace()

	// MANAGED
	stateManaged := operatorv1.Managed
	conditionsManagedReady := []gTypes.GomegaMatcher{
		// Validate that the component's management state is updated correctly
		jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, stateManaged),
		// Validate the "Ready" condition for the component
		jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionTrue),
	}

	// Update the management state of the component in the DataScienceCluster.
	tc.EventuallyResourceCreatedOrUpdated(
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
	tc.EventuallyResourceCreatedOrUpdated(
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

	tc.EventuallyResourceCreatedOrUpdated(
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
		tc.setupKueueLegacyManagedNamespace()

		// MANAGED
		stateManaged := operatorv1.Managed
		conditionsManaged := []gTypes.GomegaMatcher{
			// Validate that the component's management state is updated correctly
			jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, stateManaged),
			// Validate the "Ready" condition for the component
			jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionTrue),
		}

		// Update the management state of the component in the DataScienceCluster to Managed.
		tc.EventuallyResourceCreatedOrUpdated(
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
		tc.EventuallyResourceCreatedOrUpdated(
			WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
			WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, componentName, stateRemoved)),
			WithCondition(And(conditionsRemoved...)),
		)

		// During Removed state, validate that default Kueue resources are left untouched.
		tc.ensureClusterAndLocalQueueExist(kueueTestLegacyManagedNamespace)

		if migrateConfig {
			// Validate that Kueue's ConfigMap still exists
			tc.g.Get(
				gvk.ConfigMap, types.NamespacedName{Name: kueue.KueueConfigMapName, Namespace: tc.AppsNamespace},
			).Eventually().ShouldNot(
				BeNil(),
			)
		} else {
			// Validate that Kueue's ConfigMap is gone
			tc.g.Get(
				gvk.ConfigMap, types.NamespacedName{Name: kueue.KueueConfigMapName, Namespace: tc.AppsNamespace},
			).Eventually().Should(
				BeNil(),
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
		tc.EventuallyResourceCreatedOrUpdated(
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

// ValidateKueueWebhookValidation validates the Kueue validating webhook behavior using table-driven tests.
func (tc *KueueTestCtx) ValidateKueueWebhookValidation(t *testing.T) {
	t.Helper()

	// Ensure Kueue is in Managed state
	tc.UpdateComponentStateInDataScienceCluster(operatorv1.Managed)

	// Ensure the managed namespace exists
	tc.setupKueueManagedNamespace()

	// Create a non-managed namespace for testing
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: kueueTestWebhookNonManagedNamespace}),
		WithCustomErrorMsg("Failed to create non-managed test namespace"),
	)

	// Setup cleanup for non-managed namespace
	t.Cleanup(func() {
		tc.DeleteResource(
			WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: kueueTestWebhookNonManagedNamespace}),
		)
	})

	// Helper function to create and test notebook
	testNotebook := func(name, namespace, expectedError, errorMsg string, labels map[string]string, shouldBlock bool) func(*testing.T) {
		return func(t *testing.T) {
			t.Helper()

			// Create notebook with labels if provided
			notebook := envtestutil.NewNotebook(name, namespace)
			if labels != nil {
				notebook = envtestutil.NewNotebook(name, namespace, envtestutil.WithLabels(labels))
			}

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

	// Remove Kueue test resources
	cleanupKueueTestResources(t, tc.TestContext)
}

// ValidateHardwareProfileWebhookValidation validates the hardware profile webhook behavior using table-driven tests.
func (tc *KueueTestCtx) ValidateHardwareProfileWebhookValidation(t *testing.T) {
	t.Helper()

	// Create a non-managed namespace for hardware profile testing (avoids Kueue validation interference)
	tc.setupNamespace(kueueTestHardwareProfileNamespace, false, false)

	// Setup cleanup for the test namespace
	t.Cleanup(func() {
		tc.DeleteResource(
			WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: kueueTestHardwareProfileNamespace}),
		)
	})

	// Helper struct for hardware profile test cases to reduce parameter count
	type HardwareProfileTestCase struct {
		name              string
		workloadName      string
		profileName       string
		profileSpec       *infrav1alpha1.HardwareProfileSpec
		shouldBlock       bool
		expectedError     string
		errorMsg          string
		expectedCondition gTypes.GomegaMatcher
	}

	// Common hardware profile specs for this test function
	basicProfile := &infrav1alpha1.HardwareProfileSpec{
		Identifiers: []infrav1alpha1.HardwareIdentifier{
			{
				DisplayName:  "CPU",
				Identifier:   "cpu",
				MinCount:     intstr.FromInt32(1),
				DefaultCount: intstr.FromInt32(2),
				ResourceType: "CPU",
			},
		},
	}

	resourceInjectionProfile := &infrav1alpha1.HardwareProfileSpec{
		Identifiers: []infrav1alpha1.HardwareIdentifier{
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

	nodeSchedulingProfile := &infrav1alpha1.HardwareProfileSpec{
		Identifiers: []infrav1alpha1.HardwareIdentifier{
			{
				DisplayName:  "GPU",
				Identifier:   "nvidia.com/gpu",
				MinCount:     intstr.FromInt32(1),
				DefaultCount: intstr.FromInt32(1),
				ResourceType: "Accelerator",
			},
		},
		SchedulingSpec: &infrav1alpha1.SchedulingSpec{
			SchedulingType: infrav1alpha1.NodeScheduling,
			Node: &infrav1alpha1.NodeSchedulingSpec{
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

				// Cleanup hardware profile after test
				t.Cleanup(func() {
					tc.DeleteResource(
						WithMinimalObject(gvk.HardwareProfile, types.NamespacedName{Name: testCase.profileName, Namespace: testNamespace}),
					)
				})
			}

			// Create notebook workload
			var notebook client.Object
			if testCase.profileName != "" {
				notebook = envtestutil.NewNotebook(testCase.workloadName, testNamespace, envtestutil.WithHardwareProfile(testCase.profileName))
			} else {
				notebook = envtestutil.NewNotebook(testCase.workloadName, testNamespace)
			}

			// Test webhook behavior
			if testCase.shouldBlock {
				tc.EnsureWebhookBlocksResourceCreation(
					WithObjectToCreate(notebook),
					WithInvalidValue(testCase.expectedError),
					WithCustomErrorMsg(testCase.errorMsg),
				)
			} else {
				if testCase.expectedCondition != nil {
					tc.EventuallyResourceCreatedOrUpdated(
						WithObjectToCreate(notebook),
						WithCondition(testCase.expectedCondition),
						WithCustomErrorMsg(testCase.errorMsg),
					)
				} else {
					tc.EventuallyResourceCreatedOrUpdated(
						WithObjectToCreate(notebook),
						WithCustomErrorMsg(testCase.errorMsg),
					)
				}

				// Cleanup notebook after successful creation
				t.Cleanup(func() {
					tc.DeleteResource(
						WithMinimalObject(gvk.Notebook, types.NamespacedName{Name: testCase.workloadName, Namespace: testNamespace}),
					)
				})
			}
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
	tc.setupKueueManagedNamespace()

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
	tc.EventuallyResourceCreatedOrUpdated(
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

	// MANAGED
	stateManaged := operatorv1.Managed
	conditionsManagedNotReady := []gTypes.GomegaMatcher{
		// Validate that the component's management state is updated correctly
		jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, stateManaged),
		// Validate the "Ready" condition for the component
		jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionFalse),
	}

	// Update the management state of the component in the DataScienceCluster to Managed.
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, componentName, stateManaged)),
		WithCondition(And(conditionsManagedNotReady...)),
	)

	// Uninstall ocp kueue-operator
	uninstallOperatorWithChannel(t, tc.TestContext, types.NamespacedName{Name: kueueOpName, Namespace: kueueOcpOperatorNamespace}, kueueOcpOperatorChannel)

	conditionsManagedReady := []gTypes.GomegaMatcher{
		// Validate that the component's management state is updated correctly
		jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, stateManaged),
		// Validate the "Ready" condition for the component
		jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionTrue),
	}

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(And(conditionsManagedReady...)),
	)

	// Validate default resources are still there
	tc.ensureClusterAndLocalQueueExist(kueueTestManagedNamespace)

	// Validate that Kueue configuration is still there
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.KueueConfigV1, types.NamespacedName{Name: kueue.KueueCRName}),
	)

	// Remove Kueue test resources
	cleanupKueueTestResources(t, tc.TestContext)
}

// ensureClusterAndLocalQueueExist validates that both the default ClusterQueue
// and LocalQueue resources exist in the cluster.
//
// Parameters:
//   - localQueueNamsepaceName: The LocalQueue namespaced name
func (tc *KueueTestCtx) ensureClusterAndLocalQueueExist(localQueueNamsepaceName string) {
	// Validate that ClusterQueue still exists
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ClusterQueue, types.NamespacedName{Name: kueueDefaultClusterQueueName, Namespace: metav1.NamespaceAll}),
	)

	// Validate that LocalQueue still exists for the managed namespace
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.LocalQueue, types.NamespacedName{Name: kueueDefaultLocalQueueName, Namespace: localQueueNamsepaceName}),
	)
}

// deleteAndValidateCRD deletes a specified CRD and ensures it no longer exists in the cluster.
// It uses foreground deletion propagation policy to ensure proper cleanup of dependent resources.
//
// Parameters:
//   - crdName: The name of the Custom Resource Definition to delete
func (tc *KueueTestCtx) deleteAndValidateCRD(crdName string) {
	// Delete the CRD
	propagationPolicy := metav1.DeletePropagationForeground
	tc.DeleteResource(
		WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: crdName}),
		WithClientDeleteOptions(
			&client.DeleteOptions{
				PropagationPolicy: &propagationPolicy,
			}),
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

	_, err := tc.g.Update(gvk, name,
		func(obj *unstructured.Unstructured) error {
			resources.SetAnnotation(obj, annotations.ManagedByODHOperator, strconv.FormatBool(managed))
			return nil
		},
	).Get()

	tc.g.Expect(err).ShouldNot(HaveOccurred())

	tc.g.Get(gvk, name).Eventually().Should(And(
		jq.Match(`.metadata.annotations."%s" == "%s"`, annotations.ManagedByODHOperator, strconv.FormatBool(managed)),
		jq.Match(`.metadata.ownerReferences | length == %d`, ownerReferencesCount),
	))
}

// setupNamespace creates a test namespace with optional Kueue management labeling.
// When isKueueManaged is true, the namespace is labeled to indicate it should be
// managed by Kueue, which affects webhook validation behavior for workloads created
// within that namespace.
//
// Parameters:
//   - namespaceName: The name of the namespace to create
//   - isKueueManaged: Whether to add Kueue management labels to the namespace
//   - isKueueLegacyManaged: Whether to add Kueue legacy management labels to the namespace
func (tc *KueueTestCtx) setupNamespace(namespaceName string, isKueueManaged bool, isKueueLegacyManaged bool) {
	// Create test namespace
	testNamespace := &unstructured.Unstructured{}
	testNamespace.SetGroupVersionKind(gvk.Namespace)
	testNamespace.SetName(namespaceName)

	// Labels
	namespaceLabels := map[string]string{}
	// Add Kueue managed label only if requested
	if isKueueManaged {
		namespaceLabels[cluster.KueueManagedLabelKey] = "true"
	}
	// Add Kueue legacy managed label only if requested
	if isKueueLegacyManaged {
		namespaceLabels[cluster.KueueLegacyManagedLabelKey] = "true"
	}
	if len(namespaceLabels) > 0 {
		testNamespace.SetLabels(namespaceLabels)
	}

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(testNamespace),
		WithCustomErrorMsg("Failed to create test namespace '%s'", namespaceName),
	)
}

// setupKueueManagedNamespace is a convenience wrapper for creating Kueue-managed namespaces.
func (tc *KueueTestCtx) setupKueueManagedNamespace() {
	tc.setupNamespace(kueueTestManagedNamespace, true, false)
}

// setupKueueLegacyManagedNamespace is a convenience wrapper for creating Kueue-managed namespaces.
func (tc *KueueTestCtx) setupKueueLegacyManagedNamespace() {
	tc.setupNamespace(kueueTestLegacyManagedNamespace, false, true)
}
