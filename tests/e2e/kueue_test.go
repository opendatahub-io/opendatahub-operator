package e2e_test

import (
	"strconv"
	"strings"
	"testing"

	gTypes "github.com/onsi/gomega/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/kueue"
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
	kueueOcpOperatorNamespace    = "openshift-kueue-operator" // Namespace for the Kueue Operator
	kueueOcpOperatorChannel      = "stable-v0.2"
	kueueTestManagedNamespace    = "test-kueue-managed-ns"
	kueueDefaultClusterQueueName = "default"
	kueueDefaultLocalQueueName   = "default"
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

	// Define test cases.
	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate CRDs reinstated", componentCtx.ValidateCRDReinstated},
		{"Validate pre check", componentCtx.ValidateKueuePreCheck},
		{"Validate component releases", componentCtx.ValidateComponentReleases},
		{"Validate component managed error with ocp kueue-operator installed", componentCtx.ValidateKueueManagedWhitOcpKueueOperator},
		{"Validate component unmanaged error with ocp kueue-operator not installed", componentCtx.ValidateKueueUnmanagedWithoutOcpKueueOperator},
		{"Validate component managed to unmanaged transition", componentCtx.ValidateKueueManagedToUnmanagedTransition},
		{"Validate component managed to removed to unmanaged transition with config migration", componentCtx.ValidateKueueManagedToRemovedToUnmanagedTransition(true)},
		{"Validate component managed to removed to unmanaged transition without config migration", componentCtx.ValidateKueueManagedToRemovedToUnmanagedTransition(false)},
		{"Validate component unmanaged to managed transition", componentCtx.ValidateKueueUnmanagedToManagedTransition},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
	}

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
	tc.setComponentManagementState(componentName, operatorv1.Removed)

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
		WithMinimalObject(gvk.KueueConfigV1, types.NamespacedName{Name: kueue.KueueConfigCRName}),
	)
}

// ValidateKueueUnmanagedWithoutOcpKueueOperator ensures that if the component is in Unmanaged state and ocp kueue operator is not installed, then its status is "Not Ready".
func (tc *KueueTestCtx) ValidateKueueUnmanagedWithoutOcpKueueOperator(t *testing.T) {
	t.Helper()

	componentName := strings.ToLower(tc.GVK.Kind)

	// since the test may be executed on a non-clean state, let clean it up
	// so first set the component as removed ...
	tc.setComponentManagementState(componentName, operatorv1.Removed)

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
	tc.setComponentManagementState(componentName, operatorv1.Removed)

	// ... and then cleanup tests resources
	cleanupKueueTestResources(t, tc.TestContext)

	// Create a test namespace with Kueue management annotation
	createTestManagedNamespace(tc)

	state := operatorv1.Managed

	// State must be Managed, Ready condition must be false because ocp kueue-operator is installed
	conditions := []gTypes.GomegaMatcher{
		// Validate that the component's management state is updated correctly
		jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, state),

		// Validate the "Ready" condition for the component
		jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionTrue),
	}

	// Update the management state of the component in the DataScienceCluster.
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, componentName, state)),
		WithCondition(And(conditions...)),
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

	nextState := operatorv1.Unmanaged
	nextConditions := []gTypes.GomegaMatcher{
		// Validate that the component's management state is updated correctly
		jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, nextState),

		// Validate the "Ready" condition for the component
		jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionFalse),
	}

	// Update the management state of the component in the DataScienceCluster.
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, componentName, nextState)),
		WithCondition(And(nextConditions...)),
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

	finalConditions := []gTypes.GomegaMatcher{
		// Validate that the component's management state is updated correctly
		jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, nextState),

		// Validate the "Ready" condition for the component
		jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, tc.GVK.Kind, metav1.ConditionTrue),
	}

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(And(finalConditions...)),
	)

	// Validate that Kueue configuration is created
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.KueueConfigV1, types.NamespacedName{Name: kueue.KueueConfigCRName}),
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
		tc.setComponentManagementState(componentName, operatorv1.Removed)

		// ... and then cleanup tests resources
		cleanupKueueTestResources(t, tc.TestContext)

		// Create a test namespace with Kueue management annotation
		createTestManagedNamespace(tc)

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
		ensureClusterAndLocalQueueExist(tc)

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
		ensureClusterAndLocalQueueExist(tc)

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
		ensureClusterAndLocalQueueExist(tc)

		opts := []ResourceOpts{
			WithMinimalObject(gvk.KueueConfigV1, types.NamespacedName{Name: kueue.KueueConfigCRName}),
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

// ValidateKueueUnmanagedToManagedTransition ensures the transition from Unmanaged to Managed state happens as expected.
func (tc *KueueTestCtx) ValidateKueueUnmanagedToManagedTransition(t *testing.T) {
	t.Helper()

	componentName := strings.ToLower(tc.GVK.Kind)

	// since the test may be executed on a non-clean state, let clean it up
	// so first set the component as removed ...
	tc.setComponentManagementState(componentName, operatorv1.Removed)

	// ... and then cleanup tests resources
	cleanupKueueTestResources(t, tc.TestContext)

	// Create a test namespace with Kueue management annotation
	createTestManagedNamespace(tc)

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
	ensureClusterAndLocalQueueExist(tc)

	// Validate that Kueue configuration is created
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.KueueConfigV1, types.NamespacedName{Name: kueue.KueueConfigCRName}),
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
	ensureClusterAndLocalQueueExist(tc)

	// Validate that Kueue configuration is still there
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.KueueConfigV1, types.NamespacedName{Name: kueue.KueueConfigCRName}),
	)

	// Remove Kueue test resources
	cleanupKueueTestResources(t, tc.TestContext)
}

func ensureClusterAndLocalQueueExist(tc *KueueTestCtx) {
	// Validate that ClusterQueue still exists
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ClusterQueue, types.NamespacedName{Name: kueueDefaultClusterQueueName, Namespace: metav1.NamespaceAll}),
	)

	// Validate that LocalQueue still exists for the managed namespace
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.LocalQueue, types.NamespacedName{Name: kueueDefaultLocalQueueName, Namespace: kueueTestManagedNamespace}),
	)
}

// deleteAndValidateCRD deletes a given CRD and ensures it no longer exists.
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

// createMockCRD creates a mock CRD for a given group, version, kind, and componentName.
func (tc *KueueTestCtx) createMockCRD(gvk schema.GroupVersionKind, componentName string) {
	crd := mocks.NewMockCRD(gvk.Group, gvk.Version, strings.ToLower(gvk.Kind), componentName)

	tc.EventuallyResourceCreatedOrUpdated(WithObjectToCreate(crd))
}

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

func (tc *KueueTestCtx) setComponentManagementState(componentName string, state operatorv1.ManagementState) {
	conditionsRemoved := []gTypes.GomegaMatcher{
		// Validate that the component's management state is updated correctly
		jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, state),
		// Validate the "Ready" condition for the component
		jq.Match(`.status.conditions[] | select(.type == "%sReady") | .reason == "%s"`, tc.GVK.Kind, state),
	}

	// Update the management state of the component in the DataScienceCluster to Removed.
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, componentName, state)),
		WithCondition(And(conditionsRemoved...)),
	)
}

func createTestManagedNamespace(tc *KueueTestCtx) {
	// Create a test namespace with Kueue management annotation
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: kueueTestManagedNamespace}),
		WithMutateFunc(testf.Transform(`.metadata.labels["kueue.openshift.io/managed"] = "true"`)),
	)
}
