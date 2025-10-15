package e2e_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	infrav1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1alpha1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhAnnotations "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/mocks"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const (
	defaultCodeFlareComponentName        = "default-codeflare"
	defaultModelMeshServingComponentName = "default-modelmeshserving"
	testDSCV1Name                        = "test-dsc-v1-upgrade"
	testDSCIV1Name                       = "test-dsci-v1-upgrade"
	defaultServiceMeshName               = "default-servicemesh"
)

type CRDToCreate struct {
	GVK  schema.GroupVersionKind
	Name string
}

var removedCRDToCreate = []CRDToCreate{
	{GVK: gvk.CodeFlare, Name: defaultCodeFlareComponentName},
	{GVK: gvk.ModelMeshServing, Name: defaultModelMeshServingComponentName},
}

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

	v2Tov3UpgradeTestCtx.createCRD(removedCRDToCreate)

	// Define test cases.
	testCases := []TestCase{
		{"codeflare resources preserved after support removal", v2Tov3UpgradeTestCtx.ValidateCodeFlareResourcePreservation},
		{"modelmeshserving resources preserved after support removal", v2Tov3UpgradeTestCtx.ValidateModelMeshServingResourcePreservation},
		{"ray raise error if codeflare component present in the cluster", v2Tov3UpgradeTestCtx.ValidateRayRaiseErrorIfCodeFlarePresent},
		{"servicemesh resources preserved after support removal", v2Tov3UpgradeTestCtx.ValidateServiceMeshResourcePreservation},
	}

	// Run the test suite.
	RunTestCases(t, testCases)

	for _, crd := range removedCRDToCreate {
		tc.DeleteResource(
			WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: strings.ToLower(crd.GVK.Kind) + "s." + crd.GVK.Group}),
		)
	}
}

func hardwareProfileTestSuite(t *testing.T) {
	t.Helper()

	tc, err := NewTestContext(t)
	require.NoError(t, err)

	// Create an instance of test context.
	v2Tov3UpgradeTestCtx := V2Tov3UpgradeTestCtx{
		TestContext: tc,
	}

	// Define hardware profile test cases.
	testCases := []TestCase{
		{"hardwareprofile v1alpha1 to v1 version upgrade", v2Tov3UpgradeTestCtx.HardwareProfileV1Alpha1ToV1VersionUpgrade},
		{"hardwareprofile v1 to v1alpha1 version conversion", v2Tov3UpgradeTestCtx.HardwareProfileV1ToV1Alpha1VersionConversion},
	}

	// Run the hardware profile test suite.
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

	tc.validateComponentResourcePreservation(t, gvk.CodeFlare, defaultCodeFlareComponentName)
}

func (tc *V2Tov3UpgradeTestCtx) ValidateModelMeshServingResourcePreservation(t *testing.T) {
	t.Helper()

	tc.validateComponentResourcePreservation(t, gvk.ModelMeshServing, defaultModelMeshServingComponentName)
}

func (tc *V2Tov3UpgradeTestCtx) DatascienceclusterV1CreationAndRead(t *testing.T) {
	t.Helper()

	// Clean up any existing DataScienceCluster and DSCInitialization resources before starting
	cleanupCoreOperatorResources(t, tc.TestContext)

	// Use a consistent name for this test
	dscName := testDSCV1Name

	// Create a DataScienceCluster v1 resource
	dscV1 := CreateDSCv1(dscName)

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
			jq.Match(`.spec.components | has("codeflare")`),
			jq.Match(`.spec.components | has("modelmeshserving")`),
			jq.Match(`([.spec.components.dashboard, .spec.components.workbenches, .spec.components.datasciencepipelines,
				.spec.components.kserve, .spec.components.kueue, .spec.components.ray, .spec.components.trustyai,
				.spec.components.modelregistry, .spec.components.trainingoperator, .spec.components.feastoperator,
				.spec.components.llamastackoperator] | map(.managementState) | all(. == "Removed"))`),
		)),
		WithCustomErrorMsg("Failed to read DataScienceCluster v1 resource %s", dscName),
		WithEventuallyTimeout(tc.TestTimeouts.shortEventuallyTimeout),
	)

	// Try to read the resource explicitly as v2 and verify no errors occur
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, types.NamespacedName{Name: dscName}),
		WithCondition(And(
			jq.Match(`.metadata.name == "%s"`, dscName),
			jq.Match(`.apiVersion == "%s"`, dscv2.GroupVersion.String()),
			jq.Match(`.kind == "DataScienceCluster"`),
			jq.Match(`.spec.components | has("codeflare") | not`),
			jq.Match(`.spec.components | has("modelmeshserving") | not`),
			jq.Match(`([.spec.components.dashboard, .spec.components.workbenches, .spec.components.aipipelines,
				.spec.components.kserve, .spec.components.kueue, .spec.components.ray, .spec.components.trustyai,
				.spec.components.modelregistry, .spec.components.trainingoperator, .spec.components.feastoperator,
				.spec.components.llamastackoperator] | map(.managementState) | all(. == "Removed"))`),
		)),
		WithCustomErrorMsg("Failed to read DataScienceCluster v2 resource %s", dscName),
		WithEventuallyTimeout(10*time.Second),
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

func (tc *V2Tov3UpgradeTestCtx) HardwareProfileV1Alpha1ToV1VersionUpgrade(t *testing.T) {
	t.Helper()

	hardwareProfileName := "test-hardware-profile-v1alpha1-to-v1"

	// should be able to create v1alpha1 HWProfile resource.
	hardwareProfileV1Alpha1 := CreateHardwareProfile(hardwareProfileName, tc.AppsNamespace, infrav1alpha1.GroupVersion.String())
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(hardwareProfileV1Alpha1),
		WithCustomErrorMsg("Failed to create HardwareProfile v1alpha1 resource %s", hardwareProfileName),
		WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	)

	// read with v1alpha1 API version.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.HardwareProfileV1Alpha1, types.NamespacedName{Name: hardwareProfileName, Namespace: tc.AppsNamespace}),
		WithCondition(And(
			jq.Match(`.metadata.name == "%s"`, hardwareProfileName),
			jq.Match(`.apiVersion == "%s"`, infrav1alpha1.GroupVersion.String()),
			jq.Match(`.kind == "HardwareProfile"`),
			jq.Match(`.spec.identifiers[0].displayName == "GPU"`),
			jq.Match(`.spec.identifiers[0].identifier == "nvidia.com/gpu"`),
			jq.Match(`.spec.identifiers[0].resourceType == "Accelerator"`),
			jq.Match(`.spec.scheduling.type == "Node"`),
			jq.Match(`.spec.scheduling.node.nodeSelector["kubernetes.io/arch"] == "amd64"`),
		)),
		WithCustomErrorMsg("Failed to read HardwareProfile v1alpha1 resource %s", hardwareProfileName),
		WithEventuallyTimeout(tc.TestTimeouts.shortEventuallyTimeout),
	)

	// read as v1 API version.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.HardwareProfile, types.NamespacedName{Name: hardwareProfileName, Namespace: tc.AppsNamespace}),
		WithCondition(And(
			jq.Match(`.metadata.name == "%s"`, hardwareProfileName),
			jq.Match(`.apiVersion == "%s"`, infrav1.GroupVersion.String()),
			jq.Match(`.kind == "HardwareProfile"`),
			jq.Match(`.spec.identifiers[0].displayName == "GPU"`),
			jq.Match(`.spec.identifiers[0].identifier == "nvidia.com/gpu"`),
			jq.Match(`.spec.identifiers[0].resourceType == "Accelerator"`),
			jq.Match(`.spec.scheduling.type == "Node"`),
			jq.Match(`.spec.scheduling.node.nodeSelector["kubernetes.io/arch"] == "amd64"`),
		)),
		WithCustomErrorMsg("Failed to read HardwareProfile v1 resource %s after version conversion", hardwareProfileName),
		WithEventuallyTimeout(10*time.Second),
	)

	// Cleanup - delete the test resource
	tc.DeleteResource(
		WithMinimalObject(gvk.HardwareProfileV1Alpha1, types.NamespacedName{Name: hardwareProfileName, Namespace: tc.AppsNamespace}),
		WithWaitForDeletion(true),
	)
}

func (tc *V2Tov3UpgradeTestCtx) HardwareProfileV1ToV1Alpha1VersionConversion(t *testing.T) {
	t.Helper()

	hardwareProfileName := "test-hardware-profile-v1-to-v1alpha1"

	// Create a HardwareProfile v1 resource (storage version)
	hardwareProfileV1 := CreateHardwareProfile(hardwareProfileName, tc.AppsNamespace, infrav1.GroupVersion.String())

	// Create the v1 HardwareProfile resource
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(hardwareProfileV1),
		WithCustomErrorMsg("Failed to create HardwareProfile v1 resource %s", hardwareProfileName),
		WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	)

	// Read as v1 (storage version)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.HardwareProfile, types.NamespacedName{Name: hardwareProfileName, Namespace: tc.AppsNamespace}),
		WithCondition(And(
			jq.Match(`.metadata.name == "%s"`, hardwareProfileName),
			jq.Match(`.apiVersion == "%s"`, infrav1.GroupVersion.String()),
			jq.Match(`.kind == "HardwareProfile"`),
		)),
		WithCustomErrorMsg("Failed to read HardwareProfile v1 resource %s", hardwareProfileName),
		WithEventuallyTimeout(tc.TestTimeouts.shortEventuallyTimeout),
	)

	// read as v1alpha1 (conversion from storage version)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.HardwareProfileV1Alpha1, types.NamespacedName{Name: hardwareProfileName, Namespace: tc.AppsNamespace}),
		WithCondition(And(
			jq.Match(`.metadata.name == "%s"`, hardwareProfileName),
			jq.Match(`.apiVersion == "%s"`, infrav1alpha1.GroupVersion.String()),
			jq.Match(`.kind == "HardwareProfile"`),
		)),
		WithCustomErrorMsg("Failed to read HardwareProfile as v1alpha1 after creating as v1 %s", hardwareProfileName),
		WithEventuallyTimeout(10*time.Second),
	)

	// Cleanup - delete the test resource
	tc.DeleteResource(
		WithMinimalObject(gvk.HardwareProfile, types.NamespacedName{Name: hardwareProfileName, Namespace: tc.AppsNamespace}),
		WithWaitForDeletion(true),
	)
}

func (tc *V2Tov3UpgradeTestCtx) validateComponentResourcePreservation(t *testing.T, componentGVK schema.GroupVersionKind, componentName string) {
	t.Helper()

	dsc := tc.FetchDataScienceCluster()

	componentToCreate := tc.operatorManagedComponent(componentGVK, componentName, dsc)
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(componentToCreate),
		WithCustomErrorMsg("Failed to create existing %s component for preservation test", componentGVK.Kind),
	)

	tc.triggerDSCReconciliation(t)

	// Verify component still exists after reconciliation (was not removed)
	tc.EnsureResourceExistsConsistently(
		WithMinimalObject(componentGVK, types.NamespacedName{Name: componentName}),
		WithCustomErrorMsg("%s component resource '%s' was expected to exist but was not found", componentGVK.Kind, componentName),
	)

	// Cleanup
	tc.DeleteResource(
		WithMinimalObject(componentGVK, types.NamespacedName{Name: componentName}),
		WithWaitForDeletion(true),
	)
}

func (tc *V2Tov3UpgradeTestCtx) ValidateRayRaiseErrorIfCodeFlarePresent(t *testing.T) {
	t.Helper()

	dsc := tc.FetchDataScienceCluster()
	existingComponent := tc.operatorManagedComponent(gvk.CodeFlare, defaultCodeFlareComponentName, dsc)

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(existingComponent),
		WithCustomErrorMsg("Failed to create existing %s component", gvk.CodeFlare),
	)

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
		WithEventuallyTimeout(tc.TestTimeouts.defaultEventuallyTimeout),
		WithCustomErrorMsg("Failed to trigger DSC reconciliation"),
	)
}

func (tc *V2Tov3UpgradeTestCtx) operatorManagedComponent(componentGVK schema.GroupVersionKind, componentName string, dsc *dscv2.DataScienceCluster) client.Object {
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

	return existingComponent
}

func (tc *V2Tov3UpgradeTestCtx) updateComponentStateInDataScienceCluster(t *testing.T, kind string, managementState operatorv1.ManagementState) {
	t.Helper()

	// Map DataSciencePipelines to aipipelines for v2 API
	componentFieldName := strings.ToLower(kind)
	if kind == dataSciencePipelinesKind {
		componentFieldName = aiPipelinesFieldName
	}

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.%s.managementState = "%s"`, componentFieldName, managementState)),
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
				Kueue: dscv1.DSCKueueV1{
					KueueManagementSpecV1: dscv1.KueueManagementSpecV1{
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
				Kueue: dscv1.DSCKueueV1{
					KueueManagementSpecV1: dscv1.KueueManagementSpecV1{
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
				Kueue: dscv1.DSCKueueV1{
					KueueManagementSpecV1: dscv1.KueueManagementSpecV1{
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
				Kueue: dscv1.DSCKueueV1{
					KueueManagementSpecV1: dscv1.KueueManagementSpecV1{
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

// createCRD creates a mock CRD for the given component GVK if it doesn't already exist in the cluster.
func (tc *V2Tov3UpgradeTestCtx) createCRD(crdsToCreate []CRDToCreate) {
	for _, crd := range crdsToCreate {
		// Create mock CRD for the component
		mockCRD := mocks.NewMockCRD(crd.GVK.Group, crd.GVK.Version, crd.GVK.Kind, crd.Name)

		tc.EventuallyResourceCreated(
			WithObjectToCreate(mockCRD),
			WithAcceptableErr(k8serr.IsAlreadyExists, "IsAlreadyExists"),
			WithCustomErrorMsg("Failed to create CRD for %s component", crd.GVK.Kind),
			WithEventuallyTimeout(tc.TestTimeouts.shortEventuallyTimeout),
		)
	}
}

func (tc *V2Tov3UpgradeTestCtx) ValidateServiceMeshResourcePreservation(t *testing.T) {
	t.Helper()

	tc.createCRD(gvk.ServiceMesh, defaultServiceMeshName)

	nn := types.NamespacedName{
		Name: defaultServiceMeshName,
	}

	dsci := tc.FetchDSCInitialization()

	tc.createOperatorManagedServiceMesh(defaultServiceMeshName, dsci)

	tc.triggerDSCIReconciliation(t)

	// verify ServiceMesh still exists after reconciliation
	tc.EnsureResourceExistsConsistently(WithMinimalObject(gvk.ServiceMesh, nn),
		WithCustomErrorMsg("ServiceMesh service resource '%s' was expected to exist but was not found", defaultServiceMeshName),
	)

	tc.DeleteResource(
		WithMinimalObject(gvk.ServiceMesh, nn),
		WithWaitForDeletion(true),
	)
}

func (tc *V2Tov3UpgradeTestCtx) createOperatorManagedServiceMesh(serviceMeshName string, dsci *dsciv2.DSCInitialization) {
	existingServiceMesh := resources.GvkToUnstructured(gvk.ServiceMesh)
	existingServiceMesh.SetName(serviceMeshName)

	resources.SetLabels(existingServiceMesh, map[string]string{
		labels.PlatformPartOf: strings.ToLower(gvk.DSCInitialization.Kind),
	})

	resources.SetAnnotations(existingServiceMesh, map[string]string{
		odhAnnotations.ManagedByODHOperator: "true",
		odhAnnotations.PlatformVersion:      dsci.Status.Release.Version.String(),
		odhAnnotations.PlatformType:         string(dsci.Status.Release.Name),
		odhAnnotations.InstanceGeneration:   strconv.Itoa(int(dsci.GetGeneration())),
		odhAnnotations.InstanceUID:          string(dsci.GetUID()),
	})

	err := controllerutil.SetOwnerReference(dsci, existingServiceMesh, tc.Scheme())
	tc.g.Expect(err).NotTo(HaveOccurred(),
		"Failed to set owner reference from DSCInitialization '%s' to ServiceMesh service '%s'",
		dsci.GetName(), serviceMeshName)

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(existingServiceMesh),
		WithCustomErrorMsg("Failed to create existing ServiceMesh service for preservation test"),
	)
}

func (tc *V2Tov3UpgradeTestCtx) triggerDSCIReconciliation(t *testing.T) {
	t.Helper()

	// trigger DSCI reconciliation by setting a customCABundle
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.trustedCABundle.customCABundle = "# reconcile trigger"`)),
		WithCondition(jq.Match(`.status.phase == "%s"`, status.ConditionTypeReady)),
		WithCustomErrorMsg("Failed to trigger DSCI reconciliation"),
	)

	// restore original customCABundle in DSCInitialization instance
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.trustedCABundle.customCABundle = ""`)),
		WithCondition(jq.Match(`.status.phase == "%s"`, status.ConditionTypeReady)),
		WithCustomErrorMsg("Failed to trigger DSCI reconciliation"),
	)
}
