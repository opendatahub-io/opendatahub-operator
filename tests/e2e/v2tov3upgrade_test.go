package e2e_test

import (
	"strconv"
	"strings"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhAnnotations "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const (
	defaultCodeFlareComponentName = "default-codeflare"
	testDSCV1Name                 = "test-dsc-v1-upgrade"
	testDSCIV1Name                = "test-dsci-v1-upgrade"
)

type V2Tov3UpgradeTestCtx struct {
	*TestContext
}

func v2Tov3UpgradeTestSuite(t *testing.T) {
	t.Helper()

	tc, err := NewTestContext(t)
	require.NoError(t, err)

	// Create an instance of test context.
	v2Tov3UpgradeTestCtx := V2Tov3UpgradeTestCtx{
		TestContext: tc,
	}

	// Define test cases.
	testCases := []TestCase{
		{"codeflare present in the cluster before upgrade, after upgrade not removed", v2Tov3UpgradeTestCtx.ValidateCodeFlareResourcePreservation},
		{"ray raise error if codeflare component present in the cluster", v2Tov3UpgradeTestCtx.ValidateRayRaiseErrorIfCodeFlarePresent},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

func v2Tov3UpgradeDeletingDscDsciTestSuite(t *testing.T) {
	t.Helper()

	tc, err := NewTestContext(t)
	require.NoError(t, err)

	// Create an instance of test context.
	v2Tov3UpgradeTestCtx := V2Tov3UpgradeTestCtx{
		TestContext: tc,
	}

	// Define test cases.
	testCases := []TestCase{
		{"datasciencecluster v1 creation and read", v2Tov3UpgradeTestCtx.DatascienceclusterV1CreationAndRead},
		{"dscinitialization v1 creation and read", v2Tov3UpgradeTestCtx.DscinitializationV1CreationAndRead},
		{"validate denies DSC v1 with Kueue Managed", v2Tov3UpgradeTestCtx.ValidateDeniesKueueManaged},
		{"validate denies DSC v1 update with Kueue Managed", v2Tov3UpgradeTestCtx.ValidateDeniesKueueManagedUpdate},
		{"validate allows DSC v1 with Kueue Unmanaged", v2Tov3UpgradeTestCtx.ValidateAllowsKueueUnmanaged},
		{"validate allows DSC v1 with Kueue Removed", v2Tov3UpgradeTestCtx.ValidateAllowsKueueRemoved},
		{"validate allows DSC v1 without Kueue", v2Tov3UpgradeTestCtx.ValidateAllowsWithoutKueue},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

func (tc *V2Tov3UpgradeTestCtx) ValidateCodeFlareResourcePreservation(t *testing.T) {
	t.Helper()

	tc.ValidateComponentResourcePreservation(t, gvk.CodeFlare, defaultCodeFlareComponentName)
}

func (tc *V2Tov3UpgradeTestCtx) DatascienceclusterV1CreationAndRead(t *testing.T) {
	t.Helper()

	// Clean up any existing DataScienceCluster and DSCInitialization resources before starting
	cleanupCoreOperatorResources(t, tc.TestContext)

	// Use a consistent name for this test
	dscName := testDSCV1Name

	// Create a DataScienceCluster v1 resource
	dscV1 := &dscv1.DataScienceCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DataScienceCluster",
			APIVersion: dscv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: dscName,
		},
		Spec: dscv1.DataScienceClusterSpec{
			Components: dscv1.Components{
				Dashboard: componentApi.DSCDashboard{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				Workbenches: componentApi.DSCWorkbenches{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				ModelMeshServing: componentApi.DSCModelMeshServing{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				DataSciencePipelines: componentApi.DSCDataSciencePipelines{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				CodeFlare: componentApi.DSCCodeFlare{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				Ray: componentApi.DSCRay{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				TrustyAI: componentApi.DSCTrustyAI{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				ModelRegistry: componentApi.DSCModelRegistry{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				TrainingOperator: componentApi.DSCTrainingOperator{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
			},
		},
	}

	// Create the v1 DataScienceCluster resource and verify it's created correctly
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(dscV1),
		WithCustomErrorMsg("Failed to create DataScienceCluster v1 resource %s", dscName),
		WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	)

	// Try to read the resource explicitly as v1 and verify no errors occur
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceClusterV1, types.NamespacedName{Name: dscName}),
		WithCondition(And(
			jq.Match(`.metadata.name == "%s"`, dscName),
			jq.Match(`.apiVersion == "%s"`, dscv1.GroupVersion.String()),
			jq.Match(`.kind == "DataScienceCluster"`),
		)),
		WithCustomErrorMsg("Failed to read DataScienceCluster v1 resource %s", dscName),
	)

	// Cleanup - delete the test resource
	tc.DeleteResource(
		WithMinimalObject(gvk.DataScienceClusterV1, types.NamespacedName{Name: dscName}),
		WithWaitForDeletion(true),
	)
}

func (tc *V2Tov3UpgradeTestCtx) DscinitializationV1CreationAndRead(t *testing.T) {
	t.Helper()

	// Clean up any existing DataScienceCluster and DSCInitialization resources before starting
	cleanupCoreOperatorResources(t, tc.TestContext)

	// Use a consistent name for this test
	dsciName := testDSCIV1Name

	// Create a DSCInitialization v1 resource
	dsciV1 := &dsciv1.DSCInitialization{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DSCInitialization",
			APIVersion: dsciv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: dsciName,
		},
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: tc.AppsNamespace,
			Monitoring: serviceApi.DSCIMonitoring{
				ManagementSpec: common.ManagementSpec{
					ManagementState: operatorv1.Removed,
				},
				MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{
					Namespace: tc.MonitoringNamespace,
				},
			},
			TrustedCABundle: &dsciv1.TrustedCABundleSpec{
				ManagementState: operatorv1.Managed,
				CustomCABundle:  "",
			},
			ServiceMesh: &infrav1.ServiceMeshSpec{
				ManagementState: operatorv1.Managed,
				ControlPlane: infrav1.ControlPlaneSpec{
					Name:              "data-science-smcp",
					Namespace:         "istio-system",
					MetricsCollection: "Istio",
				},
			},
		},
	}

	// Create the v1 DSCInitialization resource and verify it's created correctly
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(dsciV1),
		WithCustomErrorMsg("Failed to create DSCInitialization v1 resource %s", dsciName),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	)

	// Try to read the resource explicitly as v1 and verify no errors occur
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DSCInitializationV1, types.NamespacedName{Name: dsciName}),
		WithCondition(And(
			jq.Match(`.metadata.name == "%s"`, dsciName),
			jq.Match(`.apiVersion == "%s"`, dsciv1.GroupVersion.String()),
			jq.Match(`.kind == "DSCInitialization"`),
		)),
		WithCustomErrorMsg("Failed to read DSCInitialization v1 resource %s", dsciName),
	)

	// Cleanup - delete the test resource
	tc.DeleteResource(
		WithMinimalObject(gvk.DSCInitializationV1, types.NamespacedName{Name: dsciName}),
		WithWaitForDeletion(true),
	)
}

func (tc *V2Tov3UpgradeTestCtx) ValidateComponentResourcePreservation(t *testing.T, componentGVK schema.GroupVersionKind, componentName string) {
	t.Helper()

	nn := types.NamespacedName{
		Name: componentName,
	}

	dsc := tc.FetchDataScienceCluster()

	tc.createOperatorManagedComponent(componentGVK, componentName, dsc)

	tc.triggerDSCReconciliation(t)

	// Verify component still exists after reconciliation (was not removed)
	tc.EnsureResourceExistsConsistently(WithMinimalObject(gvk.CodeFlare, nn),
		WithCustomErrorMsg("CodeFlare component resource '%s' was expected to exist but was not found", defaultCodeFlareComponentName),
	)

	// Cleanup
	tc.DeleteResource(
		WithMinimalObject(componentGVK, nn),
		WithWaitForDeletion(true),
	)
}

func (tc *V2Tov3UpgradeTestCtx) ValidateRayRaiseErrorIfCodeFlarePresent(t *testing.T) {
	t.Helper()

	dsc := tc.FetchDataScienceCluster()
	tc.createOperatorManagedComponent(gvk.CodeFlare, defaultCodeFlareComponentName, dsc)

	tc.updateComponentStateInDataScienceCluster(t, gvk.Ray.Kind, operatorv1.Managed)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(And(
			jq.Match(
				`.status.conditions[]
				| select(.type == "ComponentsReady" and .status == "False")
				| .message == "%s"`,
				"Some components are not ready: ray",
			),
			jq.Match(
				`.status.conditions[]
				| select(.type == "RayReady" and .status == "False")
				| .message == "%s"`,
				status.CodeFlarePresentMessage,
			),
		)),
	)

	tc.DeleteResource(
		WithMinimalObject(gvk.CodeFlare, types.NamespacedName{Name: defaultCodeFlareComponentName}),
		WithWaitForDeletion(true),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(And(
			jq.Match(
				`.status.conditions[]
				| select(.type == "RayReady") | .status == "True"`,
			),
		)),
	)

	// Cleanup
	tc.updateComponentStateInDataScienceCluster(t, gvk.Ray.Kind, operatorv1.Removed)
}

func (tc *V2Tov3UpgradeTestCtx) triggerDSCReconciliation(t *testing.T) {
	t.Helper()

	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.dashboard = {}`)),
		WithCondition(jq.Match(`.metadata.generation == .status.observedGeneration`)),
		WithCustomErrorMsg("Failed to trigger DSC reconciliation"),
	)
}

func (tc *V2Tov3UpgradeTestCtx) createOperatorManagedComponent(componentGVK schema.GroupVersionKind, componentName string, dsc *dscv2.DataScienceCluster) {
	existingComponent := resources.GvkToUnstructured(componentGVK)
	existingComponent.SetName(componentName)

	resources.SetLabels(existingComponent, map[string]string{
		labels.PlatformPartOf: strings.ToLower(gvk.DataScienceCluster.Kind),
	})

	resources.SetAnnotations(existingComponent, map[string]string{
		odhAnnotations.ManagedByODHOperator: "true",
		odhAnnotations.PlatformVersion:      dsc.Status.Release.Version.String(),
		odhAnnotations.PlatformType:         string(dsc.Status.Release.Name),
		odhAnnotations.InstanceGeneration:   strconv.Itoa(int(dsc.GetGeneration())),
		odhAnnotations.InstanceUID:          string(dsc.GetUID()),
	})

	err := controllerutil.SetOwnerReference(dsc, existingComponent, tc.Scheme())
	tc.g.Expect(err).NotTo(HaveOccurred(),
		"Failed to set owner reference from DataScienceCluster '%s' to %s component '%s'",
		dsc.GetName(), componentGVK.Kind, componentName)

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(existingComponent),
		WithCustomErrorMsg("Failed to create existing %s component for preservation test", componentGVK.Kind),
	)
}

func (tc *V2Tov3UpgradeTestCtx) updateComponentStateInDataScienceCluster(t *testing.T, kind string, managementState operatorv1.ManagementState) {
	t.Helper()

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, strings.ToLower(kind), managementState)),
	)
}

// ValidateDeniesKueueManaged tests that the Validating webhook denies creation of
// DataScienceCluster v1 resources with Kueue managementState set to "Managed".
func (tc *V2Tov3UpgradeTestCtx) ValidateDeniesKueueManaged(t *testing.T) {
	t.Helper()

	// Clean up any existing DataScienceCluster resources before starting
	cleanupCoreOperatorResources(t, tc.TestContext)

	// Create a DataScienceCluster v1 resource with Kueue managementState set to "Managed"
	dscV1 := &dscv1.DataScienceCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DataScienceCluster",
			APIVersion: dscv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-dsc-v1-kueue-managed-denied",
		},
		Spec: dscv1.DataScienceClusterSpec{
			Components: dscv1.Components{
				Kueue: componentApi.DSCKueue{
					KueueManagementSpec: componentApi.KueueManagementSpec{
						ManagementState: operatorv1.Managed,
					},
				},
				// Set other components to Removed to minimize test complexity
				Dashboard: componentApi.DSCDashboard{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				Workbenches: componentApi.DSCWorkbenches{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
			},
		},
	}

	// Expect the Validating webhook to deny the creation
	tc.EnsureWebhookBlocksResourceCreation(
		WithObjectToCreate(dscV1),
		WithInvalidValue("Managed"),
		WithFieldName("managementState"),
		WithCustomErrorMsg("Expected validation webhook to deny DataScienceCluster v1 with Kueue managementState set to Managed"),
	)
}

// ValidateDeniesKueueManagedUpdate tests that the Validating webhook denies updates to
// DataScienceCluster v1 resources when changing Kueue managementState to "Managed".
func (tc *V2Tov3UpgradeTestCtx) ValidateDeniesKueueManagedUpdate(t *testing.T) {
	t.Helper()

	// Clean up any existing DataScienceCluster resources before starting
	cleanupCoreOperatorResources(t, tc.TestContext)

	dscName := "test-dsc-v1-kueue-update-denied"

	// First, create a DataScienceCluster v1 resource with Kueue managementState set to "Removed"
	dscV1 := &dscv1.DataScienceCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DataScienceCluster",
			APIVersion: dscv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: dscName,
		},
		Spec: dscv1.DataScienceClusterSpec{
			Components: dscv1.Components{
				Kueue: componentApi.DSCKueue{
					KueueManagementSpec: componentApi.KueueManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				// Set other components to Removed to minimize test complexity
				Dashboard: componentApi.DSCDashboard{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				Workbenches: componentApi.DSCWorkbenches{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
			},
		},
	}

	// Create the initial resource with Kueue set to Removed
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(dscV1),
		WithCustomErrorMsg("Failed to create initial DataScienceCluster v1 with Kueue Removed"),
		WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	)

	// Verify the resource was created successfully
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceClusterV1, types.NamespacedName{Name: dscName}),
		WithCondition(And(
			jq.Match(`.metadata.name == "%s"`, dscName),
			jq.Match(`.spec.components.kueue.managementState == "Removed"`),
		)),
		WithCustomErrorMsg("Failed to verify initial DataScienceCluster v1 resource was created"),
	)

	// Now attempt to update the resource to set Kueue managementState to "Managed"
	// This should be denied by the validation webhook
	tc.EnsureWebhookBlocksResourceUpdate(
		WithMinimalObject(gvk.DataScienceClusterV1, types.NamespacedName{Name: dscName}),
		WithMutateFunc(testf.Transform(`.spec.components.kueue.managementState = "Managed"`)),
		WithInvalidValue("Managed"),
		WithFieldName("managementState"),
		WithCustomErrorMsg("Expected validation webhook to deny DataScienceCluster v1 update with Kueue managementState set to Managed"),
	)

	// Verify the resource still has the original state (Removed)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceClusterV1, types.NamespacedName{Name: dscName}),
		WithCondition(And(
			jq.Match(`.metadata.name == "%s"`, dscName),
			jq.Match(`.spec.components.kueue.managementState == "Removed"`),
		)),
		WithCustomErrorMsg("DataScienceCluster v1 resource should still have Kueue managementState as Removed after blocked update"),
	)

	// Cleanup - delete the test resource
	tc.DeleteResource(
		WithMinimalObject(gvk.DataScienceClusterV1, types.NamespacedName{Name: dscName}),
		WithWaitForDeletion(true),
	)
}

// ValidateAllowsKueueUnmanaged tests that the Validating webhook allows creation of
// DataScienceCluster v1 resources with Kueue managementState set to "Unmanaged".
func (tc *V2Tov3UpgradeTestCtx) ValidateAllowsKueueUnmanaged(t *testing.T) {
	t.Helper()

	// Clean up any existing DataScienceCluster resources before starting
	cleanupCoreOperatorResources(t, tc.TestContext)

	dscName := "test-dsc-v1-kueue-unmanaged-allowed"

	// Create a DataScienceCluster v1 resource with Kueue managementState set to "Unmanaged"
	dscV1 := &dscv1.DataScienceCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DataScienceCluster",
			APIVersion: dscv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: dscName,
		},
		Spec: dscv1.DataScienceClusterSpec{
			Components: dscv1.Components{
				Kueue: componentApi.DSCKueue{
					KueueManagementSpec: componentApi.KueueManagementSpec{
						ManagementState: operatorv1.Unmanaged,
					},
				},
				// Set other components to Removed to minimize test complexity
				Dashboard: componentApi.DSCDashboard{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				Workbenches: componentApi.DSCWorkbenches{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
			},
		},
	}

	// Expect the Validating webhook to allow the creation
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(dscV1),
		WithCustomErrorMsg("Expected validation webhook to allow DataScienceCluster v1 with Kueue managementState set to Unmanaged"),
		WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	)

	// Verify the resource was created successfully
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceClusterV1, types.NamespacedName{Name: dscName}),
		WithCondition(And(
			jq.Match(`.metadata.name == "%s"`, dscName),
			jq.Match(`.spec.components.kueue.managementState == "Unmanaged"`),
		)),
		WithCustomErrorMsg("Failed to verify DataScienceCluster v1 resource with Kueue Unmanaged was created"),
	)

	// Cleanup - delete the test resource
	tc.DeleteResource(
		WithMinimalObject(gvk.DataScienceClusterV1, types.NamespacedName{Name: dscName}),
		WithWaitForDeletion(true),
	)
}

// ValidateAllowsKueueRemoved tests that the Validating webhook allows creation of
// DataScienceCluster v1 resources with Kueue managementState set to "Removed".
func (tc *V2Tov3UpgradeTestCtx) ValidateAllowsKueueRemoved(t *testing.T) {
	t.Helper()

	// Clean up any existing DataScienceCluster resources before starting
	cleanupCoreOperatorResources(t, tc.TestContext)

	dscName := "test-dsc-v1-kueue-removed-allowed"

	// Create a DataScienceCluster v1 resource with Kueue managementState set to "Removed"
	dscV1 := &dscv1.DataScienceCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DataScienceCluster",
			APIVersion: dscv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: dscName,
		},
		Spec: dscv1.DataScienceClusterSpec{
			Components: dscv1.Components{
				Kueue: componentApi.DSCKueue{
					KueueManagementSpec: componentApi.KueueManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				// Set other components to Removed to minimize test complexity
				Dashboard: componentApi.DSCDashboard{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				Workbenches: componentApi.DSCWorkbenches{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
			},
		},
	}

	// Expect the Validating webhook to allow the creation
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(dscV1),
		WithCustomErrorMsg("Expected validation webhook to allow DataScienceCluster v1 with Kueue managementState set to Removed"),
		WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	)

	// Verify the resource was created successfully
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceClusterV1, types.NamespacedName{Name: dscName}),
		WithCondition(And(
			jq.Match(`.metadata.name == "%s"`, dscName),
			jq.Match(`.spec.components.kueue.managementState == "Removed"`),
		)),
		WithCustomErrorMsg("Failed to verify DataScienceCluster v1 resource with Kueue Removed was created"),
	)

	// Cleanup - delete the test resource
	tc.DeleteResource(
		WithMinimalObject(gvk.DataScienceClusterV1, types.NamespacedName{Name: dscName}),
		WithWaitForDeletion(true),
	)
}

// ValidateAllowsWithoutKueue tests that the Validating webhook allows creation of
// DataScienceCluster v1 resources that don't specify the Kueue component at all.
func (tc *V2Tov3UpgradeTestCtx) ValidateAllowsWithoutKueue(t *testing.T) {
	t.Helper()

	// Clean up any existing DataScienceCluster resources before starting
	cleanupCoreOperatorResources(t, tc.TestContext)

	dscName := "test-dsc-v1-no-kueue-allowed"

	// Create a DataScienceCluster v1 resource without specifying Kueue component
	dscV1 := &dscv1.DataScienceCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DataScienceCluster",
			APIVersion: dscv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: dscName,
		},
		Spec: dscv1.DataScienceClusterSpec{
			Components: dscv1.Components{
				// Only specify Dashboard, no Kueue component
				Dashboard: componentApi.DSCDashboard{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				Workbenches: componentApi.DSCWorkbenches{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
			},
		},
	}

	// Expect the Validating webhook to allow the creation
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(dscV1),
		WithCustomErrorMsg("Expected validation webhook to allow DataScienceCluster v1 without Kueue component"),
		WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	)

	// Cleanup - delete the test resource
	tc.DeleteResource(
		WithMinimalObject(gvk.DataScienceClusterV1, types.NamespacedName{Name: dscName}),
		WithWaitForDeletion(true),
	)
}
