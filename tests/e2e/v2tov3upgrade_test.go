package e2e_test

import (
	"strconv"
	"strings"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
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
	defaultServiceMeshName               = "default-servicemesh"
)

type CRDToCreate struct {
	GVK  schema.GroupVersionKind
	Name string
}

var removedCRDToCreate = []CRDToCreate{
	{GVK: gvk.CodeFlare, Name: defaultCodeFlareComponentName},
	{GVK: gvk.ModelMeshServing, Name: defaultModelMeshServingComponentName},
	{GVK: gvk.ServiceMesh, Name: defaultServiceMeshName},
}

type V2Tov3UpgradeTestCtx struct {
	*TestContext
}

func v2Tov3UpgradeTestSuite(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)

	tc, err := NewTestContext(t)
	require.NoError(t, err)

	// Create an instance of test context.
	v2Tov3UpgradeTestCtx := V2Tov3UpgradeTestCtx{
		TestContext: tc,
	}

	// Register cleanup before creation so it runs even if createCRD() fails midway
	t.Cleanup(func() {
		for _, crd := range removedCRDToCreate {
			tc.DeleteResource(
				WithMinimalObject(gvk.CustomResourceDefinition, types.NamespacedName{Name: strings.ToLower(crd.GVK.Kind) + "s." + crd.GVK.Group}),
				WithIgnoreNotFound(true),
			)
		}
	})

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
}

func v2Tov3UpgradeDeletingDscDsciTestSuite(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier3)

	tc, err := NewTestContext(t)
	require.NoError(t, err)

	// Create an instance of test context.
	v2Tov3UpgradeTestCtx := V2Tov3UpgradeTestCtx{
		TestContext: tc,
	}

	// Define test cases.
	testCases := []TestCase{
		{"validate allows argoWorkflowsControllers datasciencepipelines DSC v1", v2Tov3UpgradeTestCtx.ValidateArgoWorkflowsControllersDatasciencepipelinesDSCV1},
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

func (tc *V2Tov3UpgradeTestCtx) validateComponentResourcePreservation(t *testing.T, componentGVK schema.GroupVersionKind, componentName string) {
	t.Helper()

	dsc := tc.FetchDataScienceCluster()

	componentToCreate := tc.operatorManagedComponent(componentGVK, componentName, dsc)
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(componentToCreate),
		WithCleanup(t),
		WithCustomErrorMsg("Failed to create existing %s component for preservation test", componentGVK.Kind),
	)

	tc.triggerDSCReconciliation(t)

	// Verify component still exists after reconciliation (was not removed)
	tc.EnsureResourceExistsConsistently(
		WithMinimalObject(componentGVK, types.NamespacedName{Name: componentName}),
		WithCustomErrorMsg("%s component resource '%s' was expected to exist but was not found", componentGVK.Kind, componentName),
	)
}

func (tc *V2Tov3UpgradeTestCtx) ValidateRayRaiseErrorIfCodeFlarePresent(t *testing.T) {
	t.Helper()

	// Register cleanup to restore Ray to Removed state even on test failure
	t.Cleanup(func() {
		tc.updateComponentStateInDataScienceCluster(t, gvk.Ray.Kind, operatorv1.Removed)
	})

	dsc := tc.FetchDataScienceCluster()
	existingComponent := tc.operatorManagedComponent(gvk.CodeFlare, defaultCodeFlareComponentName, dsc)

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(existingComponent),
		WithCleanup(t),
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

	nn := types.NamespacedName{
		Name: defaultServiceMeshName,
	}

	dsci := tc.FetchDSCInitialization()

	tc.createOperatorManagedServiceMesh(defaultServiceMeshName, dsci)

	// Register cleanup at creation time - runs even on test failure/timeout
	t.Cleanup(func() {
		tc.DeleteResource(
			WithMinimalObject(gvk.ServiceMesh, nn),
			WithIgnoreNotFound(true),
			WithRemoveFinalizersOnDelete(true),
		)
	})

	tc.triggerDSCIReconciliation(t)

	// verify ServiceMesh still exists after reconciliation
	tc.EnsureResourceExistsConsistently(WithMinimalObject(gvk.ServiceMesh, nn),
		WithCustomErrorMsg("ServiceMesh service resource '%s' was expected to exist but was not found", defaultServiceMeshName),
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

// ValidateArgoWorkflowsControllersDatasciencepipelinesDSCV1 ensures the DataSciencePipelines component is ready if the
// argoWorkflowsControllersSpec options are set to "Removed" when using v1 API (datasciencepipelines field).
func (tc *V2Tov3UpgradeTestCtx) ValidateArgoWorkflowsControllersDatasciencepipelinesDSCV1(t *testing.T) {
	t.Helper()

	// Clean up any existing DataScienceCluster resources before starting
	cleanupCoreOperatorResources(t, tc.TestContext)

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(CreateDSCI(tc.DSCInitializationNamespacedName.Name, tc.AppsNamespace, tc.MonitoringNamespace)),
		WithCondition(jq.Match(`.status.phase == "%s"`, status.ConditionTypeReady)),
		WithCustomErrorMsg("Failed to create DSCInitialization resource %s", tc.DSCInitializationNamespacedName.Name),
	)

	dscName := "test-dsc-v1-datasciencepipelines"

	// Register cleanup at creation time - runs even on test failure/timeout
	// t.Cleanup runs in LIFO order: DSC is deleted first, then DSCI
	t.Cleanup(func() {
		tc.DeleteResource(
			WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
			WithIgnoreNotFound(true),
			WithWaitForDeletion(true),
		)
	})

	// Create a DataScienceCluster v2 resource with AIPipelines set to Managed
	dscV2 := CreateDSC(dscName, tc.WorkbenchesNamespace)
	dscV2.Spec.Components.AIPipelines.ManagementState = operatorv1.Managed

	// Expect the Validating webhook to allow the creation
	tc.EventuallyResourceCreated(
		WithObjectToCreate(dscV2),
		WithCustomErrorMsg("Expected validation webhook to allow DataScienceCluster v2 with AIPipelines set to Managed"),
		WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
	)

	t.Cleanup(func() {
		tc.DeleteResource(
			WithMinimalObject(gvk.DataScienceClusterV1, types.NamespacedName{Name: dscName}),
			WithIgnoreNotFound(true),
			WithWaitForDeletion(true),
		)
	})

	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceClusterV1, types.NamespacedName{Name: dscName}),
		WithMutateFunc(testf.Transform(`.spec.components.datasciencepipelines.argoWorkflowsControllers.managementState = "%s"`, operatorv1.Removed)),
		WithCondition(
			And(
				// Verify DSC v1 condition type exists
				jq.Match(`.status.conditions[] | select(.type == "DataSciencePipelinesReady") | .status == "True"`),
				// Verify DSC v2 condition type does NOT exist
				jq.Match(`[.status.conditions[] | select(.type == "AIPipelinesReady")] | length == 0`),
			),
		),
	)
}
