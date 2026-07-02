package e2e_test

import (
	"strings"
	"testing"

	"github.com/rs/xid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const dashboardControllerDeployment = "dashboard-operator"

type DashboardTestCtx struct {
	*TestContext

	moduleGVK    schema.GroupVersionKind
	moduleCRNN   types.NamespacedName
	controllerNN types.NamespacedName
}

func dashboardTestSuite(t *testing.T) {
	t.Helper()

	tc, err := NewTestContext(t)
	require.NoError(t, err)

	componentCtx := DashboardTestCtx{
		TestContext: tc,
		moduleGVK: schema.GroupVersionKind{
			Group:   componentApi.GroupVersion.Group,
			Version: componentApi.GroupVersion.Version,
			Kind:    componentApi.DashboardKind,
		},
		moduleCRNN: types.NamespacedName{Name: componentApi.DashboardInstanceName},
		controllerNN: types.NamespacedName{
			Namespace: tc.AppsNamespace,
			Name:      dashboardControllerDeployment,
		},
	}

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

	RunTestCases(t, testCases)
}

func (tc *DashboardTestCtx) enableDashboard(t *testing.T) {
	t.Helper()

	if !tc.IsXKS() {
		tc.EventuallyResourcePatched(
			WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
			WithMutateFunc(testf.Transform(`.spec.components.dashboard.managementState = "Removed"`)),
			WithCondition(jq.Match(`.spec.components.dashboard.managementState == "Removed"`)),
		)
		tc.EnsureResourceGone(WithMinimalObject(tc.moduleGVK, tc.moduleCRNN))
	}

	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.dashboard.managementState = "Managed"`)),
		WithCondition(jq.Match(`.spec.components.dashboard.managementState == "Managed"`)),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(tc.moduleGVK, tc.moduleCRNN),
		WithCondition(And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, metav1.ConditionTrue),
		)),
	)
}

func (tc *DashboardTestCtx) disableDashboard(t *testing.T) {
	t.Helper()

	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.dashboard.managementState = "Removed"`)),
		WithCondition(jq.Match(`.spec.components.dashboard.managementState == "Removed"`)),
	)

	tc.EnsureResourceGone(WithMinimalObject(tc.moduleGVK, tc.moduleCRNN))
	tc.EnsureResourceGone(WithMinimalObject(gvk.Deployment, tc.controllerNN))
}


func (tc *DashboardTestCtx) ValidateComponentEnabled(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke, Tier1)

	tc.enableDashboard(t)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, tc.controllerNN),
		WithCondition(jq.Match(`.status.readyReplicas >= 1`)),
	)
}

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
					labels.PlatformPartOf: strings.ToLower(componentApi.DashboardKind),
				}.AsSelector(),
			},
		),
		WithCondition(
			HaveEach(
				jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, componentApi.DashboardKind),
			),
		),
		WithCustomErrorMsg("Deployment resources with correct owner references should exist"),
	)
}

func (tc *DashboardTestCtx) ValidateUpdateDeploymentsResources(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke)

	deployments := tc.EnsureResourcesExist(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace}),
		WithListOptions(
			&client.ListOptions{
				Namespace: tc.AppsNamespace,
				LabelSelector: k8slabels.Set{
					labels.PlatformPartOf: strings.ToLower(componentApi.DashboardKind),
				}.AsSelector(),
			},
		),
	)

	for _, d := range deployments {
		t.Run("deployment_"+d.GetName(), func(t *testing.T) {
			t.Helper()

			replicas := ExtractAndExpectValue[int](tc.g, d, `.spec.replicas`, Not(BeNil()))

			expectedReplica := replicas + 1
			if replicas > 1 {
				expectedReplica = 1
			}

			tc.ConsistentlyResourceCreatedOrUpdated(
				WithMinimalObject(gvk.Deployment, resources.NamespacedNameFromObject(&d)),
				WithMutateFunc(testf.Transform(`.spec.replicas = %d`, expectedReplica)),
				WithCondition(jq.Match(`.spec.replicas == %d`, expectedReplica)),
			)
		})
	}
}

// ValidateOperandsDynamicallyWatchedResources ensures that operands are correctly watched for dynamic updates.
func (tc *DashboardTestCtx) ValidateOperandsDynamicallyWatchedResources(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke)

	newPt := xid.New().String()
	oldPt := ""

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.OdhApplication, types.NamespacedName{Name: "jupyter", Namespace: tc.AppsNamespace}),
		WithMutateFunc(
			func(obj *unstructured.Unstructured) error {
				oldPt = resources.SetAnnotation(obj, annotations.PlatformType, newPt)
				return nil
			},
		),
	)

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

	tc.disableDashboard(t)

	for _, crd := range crds {
		t.Run(crd.Name+"_removal", func(t *testing.T) {
			nn := types.NamespacedName{Name: crd.Name}
			tc.EnsureResourceExists(WithMinimalObject(gvk.CustomResourceDefinition, nn))
			tc.DeleteResource(
				WithMinimalObject(gvk.CustomResourceDefinition, nn),
				WithForegroundDeletion(),
				WithWaitForDeletion(true),
			)
		})
	}

	tc.enableDashboard(t)

	for _, crd := range crds {
		t.Run(crd.Name+"_reinstatement", func(t *testing.T) {
			t.Parallel()

			nn := types.NamespacedName{Name: crd.Name}
			tc.EnsureResourceExists(WithMinimalObject(gvk.CustomResourceDefinition, nn))

			if len(crd.Version) != 0 {
				tc.EnsureResourceExists(
					WithMinimalObject(gvk.CustomResourceDefinition, nn),
					WithCondition(jq.Match(`.status.storedVersions[0] == "%s"`, crd.Version)),
				)
			}
		})
	}
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

// ValidateAllDeletionRecovery runs the standard set of deletion recovery tests.
func (tc *DashboardTestCtx) ValidateAllDeletionRecovery(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke, Tier1)

	savedOpts := tc.DefaultResourceOpts
	tc.DefaultResourceOpts = []ResourceOpts{
		WithEventuallyTimeout(tc.TestTimeouts.deletionRecoveryTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	}
	defer func() { tc.DefaultResourceOpts = savedOpts }()

	partOfSelector := &client.ListOptions{
		Namespace: tc.AppsNamespace,
		LabelSelector: k8slabels.Set{
			labels.PlatformPartOf: strings.ToLower(componentApi.DashboardKind),
		}.AsSelector(),
	}

	testCases := []TestCase{
		{"ConfigMap deletion recovery", func(t *testing.T) {
			t.Helper()
			tc.validateResourceDeletionRecovery(t, gvk.ConfigMap, types.NamespacedName{Namespace: tc.AppsNamespace}, partOfSelector)
		}},
		{"Service deletion recovery", func(t *testing.T) {
			t.Helper()
			tc.validateResourceDeletionRecovery(t, gvk.Service, types.NamespacedName{Namespace: tc.AppsNamespace}, partOfSelector)
		}},
		{"Deployment deletion recovery", func(t *testing.T) {
			t.Helper()

			deployments := tc.FetchResources(
				WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace}),
				WithListOptions(partOfSelector),
			)

			if len(deployments) == 0 {
				t.Logf("No Deployment resources found for dashboard, skipping")
				return
			}

			for _, deployment := range deployments {
				t.Run("deployment_"+deployment.GetName(), func(t *testing.T) {
					t.Helper()

					recreated := tc.EnsureResourceDeletedThenRecreated(
						WithMinimalObject(gvk.Deployment, resources.NamespacedNameFromObject(&deployment)),
					)

					tc.EnsureResourceExists(
						WithMinimalObject(gvk.Deployment, resources.NamespacedNameFromObject(recreated)),
						WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`,
							status.ConditionTypeAvailable, metav1.ConditionTrue)),
						WithCustomErrorMsg("Recreated deployment should have Available condition"),
					)
				})
			}
		}},
		{"Route deletion recovery", func(t *testing.T) {
			t.Helper()
			tc.validateResourceDeletionRecovery(t, gvk.Route, types.NamespacedName{Namespace: tc.AppsNamespace}, partOfSelector)
		}},
	}

	RunTestCases(t, testCases)
}

func (tc *DashboardTestCtx) validateResourceDeletionRecovery(
	t *testing.T,
	resourceGVK schema.GroupVersionKind,
	nn types.NamespacedName,
	listOpts *client.ListOptions,
) {
	t.Helper()

	existingResources := tc.FetchResources(
		WithMinimalObject(resourceGVK, nn),
		WithListOptions(listOpts),
	)

	if len(existingResources) == 0 {
		t.Logf("No %s resources found for dashboard, skipping", resourceGVK.Kind)
		return
	}

	for _, resource := range existingResources {
		t.Run(resourceGVK.Kind+"_"+resource.GetName(), func(t *testing.T) {
			t.Helper()

			tc.EnsureResourceDeletedThenRecreated(
				WithMinimalObject(resourceGVK, resources.NamespacedNameFromObject(&resource)),
			)
		})
	}
}

func (tc *DashboardTestCtx) ValidateComponentDisabled(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke, Tier1)

	tc.EnsureResourceExists(WithMinimalObject(tc.moduleGVK, tc.moduleCRNN))

	tc.disableDashboard(t)

	tc.EnsureResourcesGone(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace}),
		WithListOptions(
			&client.ListOptions{
				Namespace: tc.AppsNamespace,
				LabelSelector: k8slabels.Set{
					labels.PlatformPartOf: strings.ToLower(componentApi.DashboardKind),
				}.AsSelector(),
			},
		),
		WithEventuallyTimeout(tc.TestTimeouts.componentReadinessTimeout),
	)

	tc.EnsureResourceGone(WithMinimalObject(tc.moduleGVK, tc.moduleCRNN))
}
