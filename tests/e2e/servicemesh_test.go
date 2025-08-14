package e2e_test

import (
	"strings"
	"testing"

	gTypes "github.com/onsi/gomega/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

const (
	authorinoDefaultName      = "authorino"
	authorinoDefaultNamespace = "opendatahub-auth-provider"
)

type ServiceMeshTestCtx struct {
	*TestContext
	GVK            schema.GroupVersionKind
	NamespacedName types.NamespacedName
}

func serviceMeshControllerTestSuite(t *testing.T) {
	t.Helper()

	// Initialize the test context.
	tc, err := NewTestContext(t)
	require.NoError(t, err, "Failed to initialize test context")

	// Create an instance of test context.
	smCtx := ServiceMeshTestCtx{
		TestContext: tc,
		GVK:         gvk.ServiceMesh,
		NamespacedName: types.NamespacedName{
			Name: serviceApi.ServiceMeshInstanceName,
		},
	}

	skipIfServiceMeshRemoved(t, tc)

	// Define test cases.
	testCases := []TestCase{
		{"Validate ServiceMesh CR creation", smCtx.ValidateServiceMeshCRCreation},
		{"Validate No ServiceMesh FeatureTrackers", smCtx.ValidateNoServiceMeshFeatureTrackers},
		{"Validate Authorino resources", smCtx.ValidateAuthorinoResources},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

func (tc *ServiceMeshTestCtx) ValidateServiceMeshCRCreation(t *testing.T) {
	t.Helper()

	// Ensure that exactly one ServiceMesh CR exists.
	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.ServiceMesh, tc.NamespacedName),
		WithCondition(HaveLen(1)),
		WithCustomErrorMsg(
			"Expected exactly one resource '%s' of kind '%s', but found a different number of resources.",
			resources.FormatNamespacedName(tc.NamespacedName),
			gvk.ServiceMesh.Kind,
		),
	)
}

// ValidateNoServiceMeshFeatureTrackers ensures there are no FeatureTrackers for ServiceMesh.
func (tc *ServiceMeshTestCtx) ValidateNoServiceMeshFeatureTrackers(t *testing.T) {
	t.Helper()

	tc.EnsureResourcesDoNotExist(
		WithMinimalObject(gvk.FeatureTracker, tc.NamespacedName),
		WithListOptions(&client.ListOptions{
			Namespace: tc.AppsNamespace,
			LabelSelector: k8slabels.SelectorFromSet(
				k8slabels.Set{
					labels.PlatformPartOf: strings.ToLower(tc.GVK.Kind),
				},
			),
		}),
		WithCustomErrorMsg("Expected no ServiceMesh-related FeatureTracker resources to be present"),
	)
}

// ValidateAuthorinoResources ensures authorino resource is ready and authorino deployment template was properly annotated.
func (tc *ServiceMeshTestCtx) ValidateAuthorinoResources(t *testing.T) {
	t.Helper()

	namespacedAuthorino := types.NamespacedName{Name: authorinoDefaultName, Namespace: authorinoDefaultNamespace}
	// ensure authorino operator is installed
	tc.EnsureOperatorInstalled(types.NamespacedName{Name: authorinoOpName, Namespace: openshiftOperatorsNamespace}, true)

	// Validate the "Ready" condition for authorino resource
	conditions := []gTypes.GomegaMatcher{
		jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "%s"`, metav1.ConditionTrue),
	}
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Authorino, namespacedAuthorino),
		WithCondition(And(conditions...)),
	)

	// Validate authorino deployment was annotated
	conditionsDeployment := []gTypes.GomegaMatcher{
		jq.Match(`.spec.template.metadata.labels | has("sidecar.istio.io/inject") and .["sidecar.istio.io/inject"] == "true"`),
	}
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, namespacedAuthorino),
		WithCondition(And(conditionsDeployment...)),
	)

	// Validate authorino deployment is re-annotated after deletion
	tc.DeleteResource(
		WithMinimalObject(gvk.Deployment, namespacedAuthorino),
	)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, namespacedAuthorino),
		WithCondition(And(conditionsDeployment...)),
	)
}

func skipIfServiceMeshRemoved(t *testing.T, tc *TestContext) {
	t.Helper()

	// Retrieve DSCInitialization resource.
	dsci := tc.FetchDSCInitialization()

	// Skip tests if ManagementState is 'Removed'.
	if dsci.Spec.ServiceMesh.ManagementState == operatorv1.Removed {
		t.Skip("ServiceMesh ManagementState is 'Removed', skipping all ServiceMesh-related tests.")
	}
}
