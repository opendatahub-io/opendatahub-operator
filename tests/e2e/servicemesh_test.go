package e2e_test

import (
	"strings"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"

	. "github.com/onsi/gomega"
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

func skipIfServiceMeshRemoved(t *testing.T, tc *TestContext) {
	t.Helper()

	// Retrieve DSCInitialization resource.
	dsci := tc.FetchDSCInitialization()

	// Skip tests if ManagementState is 'Removed'.
	if dsci.Spec.ServiceMesh.ManagementState == operatorv1.Removed {
		t.Skip("ServiceMesh ManagementState is 'Removed', skipping all ServiceMesh-related tests.")
	}
}
