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

	ct, err := NewModuleTestCtx(t, gvk.Dashboard, componentApi.DashboardInstanceName)
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
		{"Validate VAP blocks dashboard HardwareProfile and AcceleratorProfile creation", componentCtx.ValidateVAPBlocksDashboardCRCreation},
		{"Validate resource deletion recovery", componentCtx.ValidateAllDeletionRecovery},
		{"Validate component disabled", componentCtx.ValidateComponentDisabled},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateOperandsOwnerReferences overrides the ComponentTestCtx method to
// accept both Dashboard and DataScienceCluster as valid owners. With the
// module architecture the controller deployment is owned by DSC (deployed
// by the ODH Operator via Helm), while operand deployments are owned by
// the Dashboard CR (deployed by the dashboard-operator).
func (tc *DashboardTestCtx) ValidateOperandsOwnerReferences(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke)

	if tc.IsXKS() {
		t.Skip("Skipping test because operands ownership by component CR is not enforced/guaranteed on XKS platform")
	}

	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace}),
		WithListOptions(
			&client.ListOptions{
				Namespace: tc.AppsNamespace,
				LabelSelector: k8slabels.Set{
					labels.PlatformPartOf: strings.ToLower(tc.GVK.Kind),
				}.AsSelector(),
			},
		),
		WithCondition(
			HaveEach(
				SatisfyAny(
					jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, tc.GVK.Kind),
					jq.Match(`.metadata.ownerReferences[0].kind == "Platform"`),
				),
			),
		),
		WithCustomErrorMsg("Deployment resources with correct owner references should exist"),
	)
}

// ValidateUpdateDeploymentsResources overrides the ComponentTestCtx method to
// only test the controller deployment (owned by DSC). Operand deployments
// (e.g. rhods-dashboard) are owned and reconciled by the dashboard-operator,
// so external replica changes are expected to be reverted — that is correct
// module behavior, not a test failure.
func (tc *DashboardTestCtx) ValidateUpdateDeploymentsResources(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke)

	controllerDep := tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Namespace: tc.AppsNamespace,
			Name:      "dashboard-operator",
		}),
	)

	replicas := ExtractAndExpectValue[int](tc.g, *controllerDep, `.spec.replicas`, Not(BeNil()))

	expectedReplica := replicas + 1
	if replicas > 1 {
		expectedReplica = 1
	}

	tc.ConsistentlyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Namespace: tc.AppsNamespace,
			Name:      "dashboard-operator",
		}),
		WithMutateFunc(testf.Transform(`.spec.replicas = %d`, expectedReplica)),
		WithCondition(jq.Match(`.spec.replicas == %d`, expectedReplica)),
	)
}

// ValidateOperandsDynamicallyWatchedResources ensures that operands are correctly watched for dynamic updates.
func (tc *DashboardTestCtx) ValidateOperandsDynamicallyWatchedResources(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke)

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

	skipUnless(t, Tier1)

	crds := []CRD{
		{Name: "odhapplications.dashboard.opendatahub.io", Version: ""},
		{Name: "odhdocuments.dashboard.opendatahub.io", Version: ""},
	}

	tc.ValidateCRDsReinstated(t, crds)
}

// ValidateVAPBlocksDashboardCRCreation verifies that ValidatingAdmissionPolicy blocks
// creation of Dashboard HardwareProfile and AcceleratorProfile CRs.
func (tc *DashboardTestCtx) ValidateVAPBlocksDashboardCRCreation(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)

	t.Run("HardwareProfile blocked", func(t *testing.T) {
		hwp := &unstructured.Unstructured{}
		hwp.SetGroupVersionKind(gvk.DashboardHardwareProfile)
		hwp.SetName("test-hwp-" + xid.New().String())
		hwp.SetNamespace(tc.AppsNamespace)
		hwp.Object["spec"] = map[string]any{
			"displayName": "Test HardwareProfile",
			"enabled":     true,
		}
		err := tc.Client().Create(tc.Context(), hwp)
		tc.g.Expect(err).To(HaveOccurred(), "Expected HardwareProfile creation to be blocked by VAP")
	})

	t.Run("AcceleratorProfile blocked", func(t *testing.T) {
		ap := &unstructured.Unstructured{}
		ap.SetGroupVersionKind(gvk.DashboardAcceleratorProfile)
		ap.SetName("test-ap-" + xid.New().String())
		ap.SetNamespace(tc.AppsNamespace)
		ap.Object["spec"] = map[string]any{
			"displayName": "Test AcceleratorProfile",
			"enabled":     true,
			"identifier":  "nvidia.com/gpu",
		}
		err := tc.Client().Create(tc.Context(), ap)
		tc.g.Expect(err).To(HaveOccurred(), "Expected AcceleratorProfile creation to be blocked by VAP")
	})
}

// moduleSlugToName maps the app.kubernetes.io/component label value (manifest
// slug) on a ConfigMap to the module name used in the Dashboard CR's
// status.moduleStatuses map.
var moduleSlugToName = map[string]string{
	"automl":         "automl",
	"autorag":        "autorag",
	"eval-hub":       "evalHub",
	"model-registry": "modelRegistry",
	"mlflow":         "mlflow",
	"agent-ops":      "agentOps",
	"notebooks":      "notebooks",
	"gen-ai":         "genAi",
	"maas":           "maas",
}

// isModuleDeployed checks the Dashboard CR's status.moduleStatuses to determine
// whether a module identified by its manifest slug is currently deployed. This
// reflects the fully resolved state including DSC component gates, explicit
// spec.modules overrides, and inter-module dependencies.
func (tc *DashboardTestCtx) isModuleDeployed(slug string) bool {
	moduleName, known := moduleSlugToName[slug]
	if !known {
		return true
	}

	dashboard := tc.FetchResource(
		WithMinimalObject(gvk.Dashboard, tc.NamespacedName),
	)
	if dashboard == nil {
		return true
	}

	statuses, found, err := unstructured.NestedMap(dashboard.Object, "status", "moduleStatuses", moduleName)
	if err != nil || !found {
		return true
	}

	phase, _ := statuses["phase"].(string)

	return phase == "Deployed" || phase == "Degraded"
}

// ValidateAllDeletionRecovery runs the standard set of deletion recovery tests.
// Before running, it waits for all dashboard-labeled deployments to complete any
// in-progress rollouts. Earlier tests (Validate_update_operand_resources,
// Validate_dynamically_watches_operands) can trigger a rhods-dashboard rollout
// that restarts the dashboard-operator pod; if deletion recovery runs while the
// operator is mid-restart, it cannot recreate deleted ConfigMaps in time.
//
// Dashboard modules gate on DSC component states (e.g. automl requires
// aipipelines=Managed). Prior tests or parallel suites may change those states.
// The dashboard-operator does not clean up ConfigMaps for disabled modules, so
// they still exist on the cluster but will not be recreated after deletion.
// This override excludes ConfigMaps belonging to disabled modules.
func (tc *DashboardTestCtx) ValidateAllDeletionRecovery(t *testing.T) {
	t.Helper()

	// Wait for all dashboard deployments to finish rolling out
	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace}),
		WithListOptions(
			&client.ListOptions{
				Namespace: tc.AppsNamespace,
				LabelSelector: k8slabels.Set{
					labels.PlatformPartOf: strings.ToLower(tc.GVK.Kind),
				}.AsSelector(),
			},
		),
		WithCondition(
			HaveEach(
				And(
					jq.Match(`.metadata.generation == .status.observedGeneration`),
					jq.Match(`.status.updatedReplicas == .spec.replicas`),
					jq.Match(`.status.availableReplicas == .spec.replicas`),
					jq.Match(`.status.readyReplicas == .spec.replicas`),
				),
			),
		),
		WithEventuallyTimeout(tc.TestTimeouts.defaultEventuallyTimeout),
		WithCustomErrorMsg("All dashboard deployments should be fully rolled out before testing deletion recovery"),
	)

	tc.validateDashboardDeletionRecovery(t)

	// Add Dashboard-specific recovery test
	t.Run("Route deletion recovery", func(t *testing.T) {
		tc.ValidateResourceDeletionRecovery(t, gvk.Route, types.NamespacedName{Namespace: tc.AppsNamespace})
	})
}

// validateDashboardDeletionRecovery runs the standard deletion recovery tests
// with a dashboard-specific ConfigMap handler that excludes disabled modules.
func (tc *DashboardTestCtx) validateDashboardDeletionRecovery(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke, Tier1)

	savedOpts := tc.DefaultResourceOpts
	tc.DefaultResourceOpts = []ResourceOpts{
		WithEventuallyTimeout(tc.TestTimeouts.deletionRecoveryTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	}
	defer func() { tc.DefaultResourceOpts = savedOpts }()

	testCases := []TestCase{
		{"ConfigMap deletion recovery", tc.validateConfigMapDeletionRecovery},
		{"Service deletion recovery", func(t *testing.T) {
			t.Helper()
			tc.ValidateResourceDeletionRecovery(t, gvk.Service, types.NamespacedName{Namespace: tc.AppsNamespace})
		}},
		{"RBAC deletion recovery", tc.ValidateRBACDeletionRecovery},
		{"ServiceAccount deletion recovery", tc.ValidateServiceAccountDeletionRecovery},
		{"Deployment deletion recovery", tc.ValidateDeploymentDeletionRecovery},
	}

	RunTestCases(t, testCases)
}

// validateConfigMapDeletionRecovery tests that dashboard ConfigMaps are
// recreated after deletion. ConfigMaps whose app.kubernetes.io/component label
// identifies a module that is not currently deployed are skipped — the
// dashboard-operator does not recreate resources for disabled modules, but it
// also does not clean up their ConfigMaps, so they may still exist on the
// cluster as stale resources.
//
// Module deployment state is read from the Dashboard CR's status.moduleStatuses
// immediately before each subtest, which reflects the fully resolved state
// including DSC component gates, explicit spec.modules overrides, and
// inter-module dependencies.
func (tc *DashboardTestCtx) validateConfigMapDeletionRecovery(t *testing.T) {
	t.Helper()

	nn := types.NamespacedName{Namespace: tc.AppsNamespace}
	listOptions := &client.ListOptions{
		LabelSelector: k8slabels.Set{
			labels.PlatformPartOf: strings.ToLower(tc.GVK.Kind),
		}.AsSelector(),
		Namespace: nn.Namespace,
	}

	existingResources := tc.FetchResources(
		WithMinimalObject(gvk.ConfigMap, nn),
		WithListOptions(listOptions),
	)

	if len(existingResources) == 0 {
		t.Logf("No ConfigMap resources found for component %s, skipping", tc.GVK.Kind)
		return
	}

	for _, resource := range existingResources {
		t.Run("ConfigMap_"+resource.GetName(), func(t *testing.T) {
			t.Helper()

			moduleSlug := resource.GetLabels()["app.kubernetes.io/component"]
			if moduleSlug != "" && !tc.isModuleDeployed(moduleSlug) {
				t.Skipf("ConfigMap %s belongs to module %q which is not currently deployed", resource.GetName(), moduleSlug)
				return
			}

			tc.EnsureResourceDeletedThenRecreated(
				WithMinimalObject(gvk.ConfigMap, resources.NamespacedNameFromObject(&resource)),
			)
		})
	}
}
