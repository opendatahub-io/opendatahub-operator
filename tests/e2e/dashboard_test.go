package e2e_test

import (
	"strings"
	"testing"

	"github.com/rs/xid"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8slabels "k8s.io/apimachinery/pkg/labels"
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
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate dynamically watches operands", componentCtx.ValidateOperandsDynamicallyWatchedResources},
		{"Validate CRDs reinstated", componentCtx.ValidateCRDReinstated},
		{"Validate hardware profile reconcilliation", componentCtx.ValidateHardwareProfileReconciliation},
		// TODO: Disabled until these tests have been hardened (RHOAIENG-27721)
		// {"Validate deployment deletion recovery", componentCtx.ValidateDeploymentDeletionRecovery},
		// {"Validate configmap deletion recovery", componentCtx.ValidateConfigMapDeletionRecovery},
		// {"Validate service deletion recovery", componentCtx.ValidateServiceDeletionRecovery},
		// {"Validate route deletion recovery", componentCtx.ValidateRouteDeletionRecovery},
		// {"Validate serviceaccount deletion recovery", componentCtx.ValidateServiceAccountDeletionRecovery},
		// {"Validate rbac deletion recovery", componentCtx.ValidateRBACDeletionRecovery},
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
		{Name: "acceleratorprofiles.dashboard.opendatahub.io", Version: ""},
		{Name: "hardwareprofiles.dashboard.opendatahub.io", Version: ""},
		{Name: "odhapplications.dashboard.opendatahub.io", Version: ""},
		{Name: "odhdocuments.dashboard.opendatahub.io", Version: ""},
	}

	tc.ValidateCRDsReinstated(t, crds)
}

func (tc *DashboardTestCtx) ValidateHardwareProfileReconciliation(t *testing.T) {
	t.Helper()

	testHWPName := "test-hwp-" + xid.New().String()

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DashboardHardwareProfile, types.NamespacedName{Name: testHWPName, Namespace: tc.AppsNamespace}),
		WithMutateFunc(
			func(obj *unstructured.Unstructured) error {
				spec := map[string]any{
					"displayName": "Test Hardware Profile",
					"enabled":     true,
					"description": "Test hardware profile for e2e testing",
					"nodeSelector": map[string]any{
						"kubernetes.io/arch": "amd64",
					},
					"tolerations": []any{
						map[string]any{
							"key":      "test-key",
							"operator": "Equal",
							"value":    "test-value",
							"effect":   "NoSchedule",
						},
					},
				}
				if err := unstructured.SetNestedMap(obj.Object, spec, "spec"); err != nil {
					return err
				}
				return nil
			},
		),
	)

	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.HardwareProfile, types.NamespacedName{Name: testHWPName, Namespace: tc.AppsNamespace}),
		WithCondition(
			ContainElement(
				And(
					jq.Match(`.metadata.name == "%s"`, testHWPName),
					jq.Match(`.metadata.annotations."opendatahub.io/migrated-from" == "hardwareprofiles.dashboard.opendatahub.io/%s"`, testHWPName),
					jq.Match(`.metadata.annotations."opendatahub.io/display-name" == "Test Hardware Profile"`),
					jq.Match(`.metadata.annotations."opendatahub.io/description" == "Test hardware profile for e2e testing"`),
					jq.Match(`.metadata.annotations."opendatahub.io/disabled" == "false"`),
					jq.Match(`.spec.scheduling.node.nodeSelector."kubernetes.io/arch" == "amd64"`),
					jq.Match(`.spec.scheduling.node.tolerations[0].key == "test-key"`),
				),
			),
		),
	)

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DashboardHardwareProfile, types.NamespacedName{Name: testHWPName, Namespace: tc.AppsNamespace}),
		WithMutateFunc(
			func(obj *unstructured.Unstructured) error {
				resources.SetAnnotation(obj, "test-annotation", "test-value")
				resources.SetAnnotation(obj, "another-test-annotation", "another-test-value")
				return nil
			},
		),
	)

	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.HardwareProfile, types.NamespacedName{Name: testHWPName, Namespace: tc.AppsNamespace}),
		WithCondition(
			ContainElement(
				And(
					jq.Match(`.metadata.name == "%s"`, testHWPName),
					jq.Match(`.metadata.annotations."test-annotation" == "test-value"`),
					jq.Match(`.metadata.annotations."another-test-annotation" == "another-test-value"`),
					jq.Match(`.metadata.annotations."opendatahub.io/migrated-from" == "hardwareprofiles.dashboard.opendatahub.io/%s"`, testHWPName),
					jq.Match(`.metadata.annotations."opendatahub.io/display-name" == "Test Hardware Profile"`),
				),
			),
		),
	)
}
