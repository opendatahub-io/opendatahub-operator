package e2e_test

import (
	"strconv"
	"strings"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
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
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/mocks"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const (
	defaultCodeFlareComponentName        = "default-codeflare"
	defaultModelMeshServingComponentName = "default-modelmeshserving"
	testDSCV1Name                        = "test-dsc-v1-upgrade"
	testDSCIV1Name                       = "test-dsci-v1-upgrade"
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

	// Create a mock CRD for the removed components, to correctly test upgrades.
	v2Tov3UpgradeTestCtx.createRemovedComponentCRD(t)

	// Define test cases.
	testCases := []TestCase{
		{"codeflare resources preserved after support removal", v2Tov3UpgradeTestCtx.ValidateCodeFlareResourcePreservation},
		{"modelmeshserving resources preserved after support removal", v2Tov3UpgradeTestCtx.ValidateModelMeshServingResourcePreservation},
		{"ray raise error if codeflare component present in the cluster", v2Tov3UpgradeTestCtx.ValidateRayRaiseErrorIfCodeFlarePresent},
	}

	// Run the test suite.
	RunTestCases(t, testCases)

	tc.DeleteResource(
		WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: strings.ToLower(gvk.CodeFlare.Kind) + "s." + gvk.CodeFlare.Group}),
	)
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
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

func (tc *V2Tov3UpgradeTestCtx) ValidateCodeFlareResourcePreservation(t *testing.T) {
	t.Helper()

	tc.ValidateComponentResourcePreservation(t, gvk.CodeFlare, defaultCodeFlareComponentName)
}

func (tc *V2Tov3UpgradeTestCtx) ValidateModelMeshServingResourcePreservation(t *testing.T) {
	t.Helper()

	tc.ValidateComponentResourcePreservation(t, gvk.ModelMeshServing, defaultModelMeshServingComponentName)
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
			jq.Match(`([.spec.components.dashboard, .spec.components.workbenches, .spec.components.datasciencepipelines,
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

func (tc *V2Tov3UpgradeTestCtx) createRemovedComponentCRD(t *testing.T) {
	t.Helper()

	codeFlareCRD := mocks.NewMockCRD(gvk.CodeFlare.Group, gvk.CodeFlare.Version, gvk.CodeFlare.Kind, gvk.CodeFlare.Kind)
	modelMeshServingCRD := mocks.NewMockCRD(gvk.ModelMeshServing.Group, gvk.ModelMeshServing.Version, gvk.ModelMeshServing.Kind, gvk.ModelMeshServing.Kind)

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(codeFlareCRD),
		WithCustomErrorMsg("Failed to create removed component CRD"),
	)

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(modelMeshServingCRD),
		WithCustomErrorMsg("Failed to create ModelMeshServing CRD"),
	)
}
