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
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

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
		{"Validate hardware profile reconciliation", componentCtx.ValidateHardwareProfileReconciliation},
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
		{Name: "acceleratorprofiles.dashboard.opendatahub.io", Version: ""},
		{Name: "hardwareprofiles.dashboard.opendatahub.io", Version: ""},
		{Name: "odhapplications.dashboard.opendatahub.io", Version: ""},
		{Name: "odhdocuments.dashboard.opendatahub.io", Version: ""},
	}

	tc.ValidateCRDsReinstated(t, crds)
}

// ValidateAllDeletionRecovery runs the standard set of deletion recovery tests.
func (tc *DashboardTestCtx) ValidateAllDeletionRecovery(t *testing.T) {
	t.Helper()

	// Run all the standard recovery tests first
	tc.ComponentTestCtx.ValidateAllDeletionRecovery(t)

	// Add Dashboard-specific recovery test
	t.Run("Route deletion recovery", func(t *testing.T) {
		tc.ValidateResourceDeletionRecovery(t, gvk.Route)
	})
}

// TODO: Remove this entire test function once DashboardHardwareProfile CRD is deprecated and removed
// This test is only needed during the migration period from DashboardHardwareProfile to HardwareProfile.
func (tc *DashboardTestCtx) ValidateHardwareProfileReconciliation(t *testing.T) {
	t.Helper()

	const (
		testHWPDisplayName         = "Test Hardware Profile"
		testHWPDescription         = "Test hardware profile for e2e testing"
		testTolerationsKey         = "test-key"
		testTolerationsValue       = "test-value"
		testAnnotationKey          = "test-annotation"
		testAnnotationValue        = "test-value"
		anotherTestAnnotationKey   = "another-test-annotation"
		anotherTestAnnotationValue = "another-test-value"
		testNodeSelectorArch       = "amd64"
		testTolerationsEffect      = "NoSchedule"
		testTolerationsOperator    = "Equal"
	)

	testHWPName := "test-hwp-" + xid.New().String()

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DashboardHardwareProfile, types.NamespacedName{Name: testHWPName, Namespace: tc.AppsNamespace}),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.displayName = "%s"`, testHWPDisplayName),
			testf.Transform(`.spec.enabled = true`),
			testf.Transform(`.spec.description = "%s"`, testHWPDescription),
			testf.Transform(`.spec.nodeSelector = {"kubernetes.io/arch": "%s"}`, testNodeSelectorArch),
			testf.Transform(`.spec.tolerations = [{"key": "%s", "operator": "%s", "value": "%s", "effect": "%s"}]`,
				testTolerationsKey, testTolerationsOperator, testTolerationsValue, testTolerationsEffect),
		)),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.HardwareProfile, types.NamespacedName{Name: testHWPName, Namespace: tc.AppsNamespace}),
		WithCondition(And(
			jq.Match(`.metadata.name == "%s"`, testHWPName),
			jq.Match(`.metadata.annotations."opendatahub.io/migrated-from" == "hardwareprofiles.dashboard.opendatahub.io/%s"`, testHWPName),
			jq.Match(`.metadata.annotations."opendatahub.io/display-name" == "%s"`, testHWPDisplayName),
			jq.Match(`.metadata.annotations."opendatahub.io/description" == "%s"`, testHWPDescription),
			jq.Match(`.metadata.annotations."opendatahub.io/disabled" == "false"`),
			jq.Match(`.spec.scheduling.node.nodeSelector."kubernetes.io/arch" == "%s"`, testNodeSelectorArch),
			jq.Match(`.spec.scheduling.node.tolerations[0].key == "%s"`, testTolerationsKey),
		)),
	)

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DashboardHardwareProfile, types.NamespacedName{Name: testHWPName, Namespace: tc.AppsNamespace}),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.metadata.annotations."%s" = "%s"`, testAnnotationKey, testAnnotationValue),
			testf.Transform(`.metadata.annotations."%s" = "%s"`, anotherTestAnnotationKey, anotherTestAnnotationValue),
		)),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.HardwareProfile, types.NamespacedName{Name: testHWPName, Namespace: tc.AppsNamespace}),
		WithCondition(And(
			// Base conditions
			jq.Match(`.metadata.name == "%s"`, testHWPName),
			jq.Match(`.metadata.annotations."opendatahub.io/display-name" == "%s"`, testHWPDisplayName),
			jq.Match(`.metadata.annotations."%s" == "%s"`, testAnnotationKey, testAnnotationValue),
			jq.Match(`.metadata.annotations."%s" == "%s"`, anotherTestAnnotationKey, anotherTestAnnotationValue),
			jq.Match(`.metadata.annotations."opendatahub.io/migrated-from" == "hardwareprofiles.dashboard.opendatahub.io/%s"`, testHWPName),
		)),
	)
}
