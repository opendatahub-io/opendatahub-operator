package e2e_test

import (
	"strings"
	"testing"

	"github.com/blang/semver/v4"
	gTypes "github.com/onsi/gomega/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/mocks"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const (
	kueueOcpOperatorNamespace = "openshift-kueue-operator" // Namespace for the Kueue Operator
	kueueOcpOperatorChannel   = "stable-v0.1"
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
		{"Validate Kueue Dynamically create VAP and VAPB", componentCtx.ValidateKueueVAPReady},
		{"Validate CRDs reinstated", componentCtx.ValidateCRDReinstated},
		{"Validate pre check", componentCtx.ValidateKueuePreCheck},
		{"Validate component releases", componentCtx.ValidateComponentReleases},
		{"Validate component managed error with ocp kueue-operator installed", componentCtx.ValidateKueueManagedWhitOcpKueueOperator},
		{"Validate component unmanaged error with ocp kueue-operator not installed", componentCtx.ValidateKueueUnmanagedWhitoutOcpKueueOperator},
		{"Validate component managed to unmanaged transition", componentCtx.ValidateKueueManagedToUnmanagedTransition},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateKueueVAPReady ensures that Validating Admission Policies (VAP) and Bindings (VAPB) are properly configured.
func (tc *KueueTestCtx) ValidateKueueVAPReady(t *testing.T) {
	t.Helper()

	v := tc.getClusterVersion()

	if v.GTE(semver.MustParse("4.17.0")) {
		// Validate that VAP exists and has correct owner references.
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.ValidatingAdmissionPolicy, types.NamespacedName{Name: "kueue-validating-admission-policy"}),
			WithCondition(jq.Match(`.metadata.ownerReferences[0].name == "%s"`, componentApi.KueueInstanceName)),
		)

		// Validate that VAPB exists and has no owner references.
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.ValidatingAdmissionPolicyBinding, types.NamespacedName{Name: "kueue-validating-admission-policy-binding"}),
			WithCondition(jq.Match(`.metadata.ownerReferences | length == 0`)),
		)
	} else {
		// Ensure that VAP and VAPB do not exist.
		tc.EnsureResourceDoesNotExist(
			WithMinimalObject(gvk.ValidatingAdmissionPolicy, types.NamespacedName{Name: "kueue-validating-admission-policy"}),
			WithExpectedErr(&meta.NoKindMatchError{}),
		)

		tc.EnsureResourceDoesNotExist(
			WithMinimalObject(gvk.ValidatingAdmissionPolicyBinding, types.NamespacedName{Name: "kueue-validating-admission-policy-binding"}),
			WithExpectedErr(&meta.NoKindMatchError{}),
		)
	}
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

	namespacedName := types.NamespacedName{Name: kueueOpName, Namespace: kueueOcpOperatorNamespace}
	// Install ocp kueue-operator
	tc.EnsureOperatorInstalledWithChannel(namespacedName, false, kueueOcpOperatorChannel)

	componentName := strings.ToLower(tc.GVK.Kind)
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
	// Update the management state of the component in the DataScienceCluster.
	tc.ConsistentlyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(And(conditions...)),
	)

	// Uninstall Kueue operator
	uninstallOperator(t, tc.TestContext, kueueOpName, kueueOcpOperatorNamespace)
}

// ValidateKueueUnmanagedWhitoutOcpKueueOperator ensures that if the component is in Unmanaged state and ocp kueue operator is not installed, then its status is "Not Ready".
func (tc *KueueTestCtx) ValidateKueueUnmanagedWhitoutOcpKueueOperator(t *testing.T) {
	t.Helper()

	componentName := strings.ToLower(tc.GVK.Kind)
	state := operatorv1.Unmanaged

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
	// Update the management state of the component in the DataScienceCluster.
	tc.ConsistentlyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(And(conditions...)),
	)
}

// ValidateComponentEnabled ensures that if the component is in Managed state and ocp kueue operator is installed, then its status is "Not Ready".
func (tc *KueueTestCtx) ValidateKueueManagedToUnmanagedTransition(t *testing.T) {
	t.Helper()

	componentName := strings.ToLower(tc.GVK.Kind)
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

	namespacedName := types.NamespacedName{Name: kueueOpName, Namespace: kueueOcpOperatorNamespace}
	// Install ocp kueue-operator
	tc.EnsureOperatorInstalledWithChannel(namespacedName, false, kueueOcpOperatorChannel)

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

	// Uninstall Kueue operator
	uninstallOperator(t, tc.TestContext, kueueOpName, kueueOcpOperatorNamespace)
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
		WithWaitForDeletion(true),
	)
}

// createMockCRD creates a mock CRD for a given group, version, kind, and namespace.
func (tc *KueueTestCtx) createMockCRD(gvk schema.GroupVersionKind, namespace string) {
	crd := mocks.NewMockCRD(gvk.Group, gvk.Version, strings.ToLower(gvk.Kind), namespace)

	tc.EventuallyResourceCreatedOrUpdated(WithObjectToCreate(crd))
}

// getClusterVersion retrieves and parses the cluster version.
func (tc *ComponentTestCtx) getClusterVersion() semver.Version {
	cv := tc.FetchClusterVersion()
	v, err := semver.ParseTolerant(cv.Status.History[0].Version)
	tc.g.Expect(err).NotTo(HaveOccurred(), "Failed to get cluster version")

	return v
}
