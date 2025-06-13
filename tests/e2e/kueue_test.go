package e2e_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/blang/semver/v4"
	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
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
	crd := mockCRDCreation(gvk.Group, gvk.Version, strings.ToLower(gvk.Kind), namespace)

	tc.EventuallyResourceCreatedOrUpdated(WithObjectToCreate(crd))
}

// mockCRDCreation generates a mock CRD with the specified parameters.
func mockCRDCreation(group, version, kind, componentName string) *apiextv1.CustomResourceDefinition {
	return &apiextv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: strings.ToLower(fmt.Sprintf("%ss.%s", kind, group)),
			Labels: map[string]string{
				labels.ODH.Component(componentName): labels.True,
			},
		},
		Spec: apiextv1.CustomResourceDefinitionSpec{
			Group: group,
			Names: apiextv1.CustomResourceDefinitionNames{
				Kind:   kind,
				Plural: strings.ToLower(kind) + "s",
			},
			Scope: apiextv1.ClusterScoped,
			Versions: []apiextv1.CustomResourceDefinitionVersion{
				{
					Name:    version,
					Served:  true,
					Storage: true,
					Schema: &apiextv1.CustomResourceValidation{
						OpenAPIV3Schema: &apiextv1.JSONSchemaProps{
							Type: "object",
						},
					},
				},
			},
		},
	}
}

// getClusterVersion retrieves and parses the cluster version.
func (tc *ComponentTestCtx) getClusterVersion() semver.Version {
	cv := tc.FetchClusterVersion()
	v, err := semver.ParseTolerant(cv.Status.History[0].Version)
	tc.g.Expect(err).NotTo(HaveOccurred(), "Failed to get cluster version")

	return v
}
