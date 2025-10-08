package e2e_test

import (
	"strings"
	"testing"

	"github.com/rs/xid"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

type DashboardTestCtx struct {
	*ComponentTestCtx
}

func dashboardTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(t, &componentApi.Dashboard{})
	require.NoError(t, err)

	componentCtx := DashboardTestCtx{
		ComponentTestCtx: ct,
	}

	// Define test cases.
	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate VAP created", componentCtx.ValidateVAPCreated}, // TODO: Remove this when CRD is not included
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate dynamically watches operands", componentCtx.ValidateOperandsDynamicallyWatchedResources},
		{"Validate CRDs reinstated", componentCtx.ValidateCRDReinstated},
		{"Validate hardware profile creation blocked by VAP", componentCtx.ValidateHardwareProfileCreationBlockedByVAP},
		{"Validate accelerator profile creation blocked by VAP", componentCtx.ValidateAcceleratorProfileCreationBlockedByVAP},
		{"Validate resource deletion recovery", componentCtx.ValidateAllDeletionRecovery},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateOperandsDynamicallyWatchedResources ensures that operands are correctly watched for dynamic updates.
func (tc *DashboardTestCtx) ValidateOperandsDynamicallyWatchedResources(t *testing.T) {
	t.Helper()

	// Generate unique platform type values
	newPt := xid.New().String()
	oldPt := ""

	// Apply new platform type annotation and verify
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.OdhApplication, types.NamespacedName{Name: "jupyter", Namespace: tc.AppsNamespace}),
		WithMutateFunc(
			func(obj *unstructured.Unstructured) error {
				oldPt = resources.SetAnnotation(obj, annotations.PlatformType, newPt)
				return nil
			},
		),
	)

	// Ensure previously created resources retain their old platform type annotation
	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.OdhApplication, types.NamespacedName{Namespace: tc.AppsNamespace}),
		WithListOptions(
			&client.ListOptions{
				Namespace: tc.AppsNamespace,
				LabelSelector: k8slabels.Set{
					labels.PlatformPartOf: strings.ToLower(gvk.Dashboard.Kind),
				}.AsSelector(),
			},
		),
		WithCondition(
			HaveEach(
				jq.Match(`.metadata.annotations."%s" == "%s"`, annotations.PlatformType, oldPt),
			),
		),
	)
}

// ValidateCRDReinstated ensures that required CRDs are reinstated if deleted.
func (tc *DashboardTestCtx) ValidateCRDReinstated(t *testing.T) {
	t.Helper()

	crds := []CRD{
		{Name: "acceleratorprofiles.dashboard.opendatahub.io", Version: ""}, // todo: remove this when CRD is not included
		{Name: "hardwareprofiles.dashboard.opendatahub.io", Version: ""},    // todo: remove this when CRD is not included
		{Name: "odhapplications.dashboard.opendatahub.io", Version: ""},
		{Name: "odhdocuments.dashboard.opendatahub.io", Version: ""},
	}

	tc.ValidateCRDsReinstated(t, crds)
}

// ValidateVAPCreated verifies that VAP/VAPB resources are created.
func (tc *DashboardTestCtx) ValidateVAPCreated(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()
	tc.g.Expect(dsci).NotTo(BeNil(), "DSCI should exist")

	// Validate VAP/VAPB resources exist and are owned by DSCI
	vapResources := []struct {
		name string
		gvk  schema.GroupVersionKind
	}{
		{"block-dashboard-acceleratorprofile-cr", gvk.ValidatingAdmissionPolicy},
		{"block-dashboard-acceleratorprofile-cr-binding", gvk.ValidatingAdmissionPolicyBinding},
		{"block-dashboard-hardwareprofile-cr", gvk.ValidatingAdmissionPolicy},
		{"block-dashboard-hardwareprofile-cr-binding", gvk.ValidatingAdmissionPolicyBinding},
	}

	for _, resource := range vapResources {
		tc.EnsureResourceExists(
			WithMinimalObject(resource.gvk, types.NamespacedName{Name: resource.name}),
			WithCondition(And(
				jq.Match(`.metadata.name == "%s"`, resource.name),
				jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.DSCInitialization.Kind),
			)),
			WithCustomErrorMsg("%s should exist and be owned by DSCI", resource.name),
		)
	}

	// Delete one and verify it gets recreated
	vapToDelete := vapResources[0]
	tc.DeleteResource(WithMinimalObject(vapToDelete.gvk, types.NamespacedName{Name: vapToDelete.name}))

	// Verify the deleted VAP gets recreated with ownerreference to DSCI
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(vapToDelete.gvk, types.NamespacedName{Name: vapToDelete.name}),
		WithCondition(And(
			jq.Match(`.metadata.name == "%s"`, vapToDelete.name),
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.DSCInitialization.Kind),
		)),
		WithCustomErrorMsg("%s should be recreated after deletion", vapToDelete.name),
		WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
	)
}

// ValidateAllDeletionRecovery runs the standard set of deletion recovery tests.
func (tc *DashboardTestCtx) ValidateAllDeletionRecovery(t *testing.T) {
	t.Helper()

	// Run all the standard recovery tests first
	tc.ComponentTestCtx.ValidateAllDeletionRecovery(t)

	// Add Dashboard-specific recovery test
	t.Run("Route deletion recovery", func(t *testing.T) {
		tc.ValidateResourceDeletionRecovery(t, gvk.Route, types.NamespacedName{Namespace: tc.AppsNamespace})
	})
}

// todo: remove this when CRD is not included
func (tc *DashboardTestCtx) ValidateHardwareProfileCreationBlockedByVAP(t *testing.T) {
	t.Helper()

	testHWPName := "test-hwp-" + xid.New().String()
	// Create the HardwareProfile object
	// not use EventuallyResourceCreatedOrUpdated to skip timeout and should expect failure
	hwProfile := &unstructured.Unstructured{}
	hwProfile.SetGroupVersionKind(gvk.DashboardHardwareProfile)
	hwProfile.SetName(testHWPName)
	hwProfile.SetNamespace(tc.AppsNamespace)
	hwProfile.Object["spec"] = map[string]interface{}{
		"displayName": "Test HardwareProfile",
		"enabled":     true,
	}

	err := tc.Client().Create(tc.Context(), hwProfile)
	tc.g.Expect(err).To(HaveOccurred(), "Expected HardwareProfile creation to be blocked by VAP")
}

// todo: remove this when CRD is not included
func (tc *DashboardTestCtx) ValidateAcceleratorProfileCreationBlockedByVAP(t *testing.T) {
	t.Helper()

	testAPName := "test-ap-" + xid.New().String()
	apProfile := &unstructured.Unstructured{}
	apProfile.SetGroupVersionKind(gvk.DashboardAcceleratorProfile)
	apProfile.SetName(testAPName)
	apProfile.SetNamespace(tc.AppsNamespace)
	apProfile.Object["spec"] = map[string]interface{}{
		"displayName": "Test AcceleratorProfile",
		"enabled":     true,
		"identifier":  "nvidia.com/gpu",
	}

	err := tc.Client().Create(tc.Context(), apProfile)
	tc.g.Expect(err).To(HaveOccurred(), "Expected AcceleratorProfile creation to be blocked by VAP")
}
