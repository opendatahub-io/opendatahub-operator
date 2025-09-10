package e2e_test

import (
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"
)

type KserveAuthorinoTestCtx struct {
	*TestContext
}

const (
	// Default channel for Authorino operator.
	authorinoDefaultChannel = "stable"
)

// authRelatedResources defines the authorization-related resources that should NOT be created
// when Authorino is not installed.
var authRelatedResources = []struct {
	gvk schema.GroupVersionKind
	nn  types.NamespacedName
}{
	{gvk.EnvoyFilter, types.NamespacedName{Namespace: "istio-system", Name: "activator-host-header"}},
	{gvk.EnvoyFilter, types.NamespacedName{Namespace: "istio-system", Name: "envoy-oauth-temp-fix-after"}},
	{gvk.EnvoyFilter, types.NamespacedName{Namespace: "istio-system", Name: "envoy-oauth-temp-fix-before"}},
	{gvk.EnvoyFilter, types.NamespacedName{Namespace: "istio-system", Name: "kserve-inferencegraph-host-header"}},
	{gvk.AuthorizationPolicy, types.NamespacedName{Namespace: "istio-system", Name: "kserve-inferencegraph"}},
	{gvk.AuthorizationPolicy, types.NamespacedName{Namespace: "istio-system", Name: "kserve-predictor"}},
}

// TestKserveAuthorinoRegression tests the regression scenario where auth-related resources
// were created even when Authorino was not installed (RHOAI 2.19.0 issue).
func TestKserveAuthorinoRegression(t *testing.T) {
	t.Helper()

	ctx, err := NewTestContext(t)
	require.NoError(t, err)

	testCtx := KserveAuthorinoTestCtx{
		TestContext: ctx,
	}

	// Setup cleanup function similar to cleanup_test.go
	t.Cleanup(func() {
		testCtx.CleanupTestResources(t)
	})

	testCases := []TestCase{
		{"Cleanup existing resources", testCtx.CleanupTestResources},
		{"Uninstall Authorino operator", testCtx.UninstallAuthorinoOperator},
		{"Verify required operators are installed", testCtx.VerifyRequiredOperatorsInstalled},
		{"Setup DSCI with ServiceMesh", testCtx.SetupDSCIWithServiceMesh},
		{"Setup KServe with Serverless mode", testCtx.SetupKServeServerlessMode},
		{"Verify Authorino is not installed", testCtx.VerifyAuthorinoNotInstalled},
		{"Verify KServe is Ready", testCtx.VerifyKServeReady},
		{"Validate auth resources are not created in Serverless mode", testCtx.ValidateAuthResourcesNotCreatedServerless},
		{"Setup KServe with Raw deployment mode", testCtx.SetupKServeRawMode},
		{"Verify KServe is Ready in Raw mode", testCtx.VerifyKServeReady},
		{"Validate auth resources are not created in Raw mode", testCtx.ValidateAuthResourcesNotCreatedRaw},
	}

	RunTestCases(t, testCases)
}

// uninstallOperatorWithChannel delete an operator install subscription to a specific channel if exists.
func (tc *KserveAuthorinoTestCtx) uninstallOperatorWithChannel(t *testing.T, operatorNamespacedName types.NamespacedName, channel string) { //nolint:thelper,unparam
	// Check if operator subscription exists
	ro := tc.NewResourceOptions(WithMinimalObject(gvk.Subscription, operatorNamespacedName))
	operatorSubscription, err := tc.ensureResourceExistsOrNil(ro)

	if err != nil {
		t.Logf("Error checking if operator %s exists: %v", operatorNamespacedName.Name, err)
		return
	}

	if operatorSubscription != nil {
		t.Logf("Uninstalling %s operator", operatorNamespacedName.Name)

		csv, found, err := unstructured.NestedString(operatorSubscription.UnstructuredContent(), "status", "currentCSV")
		if !found || err != nil {
			t.Logf(".status.currentCSV expected to be present: %s with no error, Error: %v, but it wasn't. Deleting just the Subscription: %v", csv, err, operatorSubscription)
			tc.DeleteResource(WithMinimalObject(gvk.Subscription, operatorNamespacedName))
		} else {
			t.Logf("Deleting subscription %v and cluster service version %v", operatorNamespacedName, types.NamespacedName{Name: csv, Namespace: operatorSubscription.GetNamespace()})
			tc.DeleteResource(WithMinimalObject(gvk.Subscription, operatorNamespacedName))
			tc.DeleteResource(WithMinimalObject(gvk.ClusterServiceVersion, types.NamespacedName{Name: csv, Namespace: operatorSubscription.GetNamespace()}))
		}
	}
}

// UninstallAuthorinoOperator uninstalls the Authorino operator to ensure proper test conditions.
func (tc *KserveAuthorinoTestCtx) UninstallAuthorinoOperator(t *testing.T) {
	t.Helper()

	// Uninstall Authorino operator from openshift-operators namespace
	tc.uninstallOperatorWithChannel(t, types.NamespacedName{
		Name:      authorinoOpName,
		Namespace: "openshift-operators",
	}, authorinoDefaultChannel)

	// Also check and uninstall from operator namespace if present
	tc.uninstallOperatorWithChannel(t, types.NamespacedName{
		Name:      authorinoOpName,
		Namespace: tc.OperatorNamespace,
	}, authorinoDefaultChannel)

	// Wait for resources to be cleaned up
	time.Sleep(5 * time.Second)

	t.Logf("Authorino operator uninstallation completed")
}

// CleanupTestResources cleans up test resources using the pattern from cleanup_test.go.
func (tc *KserveAuthorinoTestCtx) CleanupTestResources(t *testing.T) {
	t.Helper()

	// Cleanup auth-related resources if they exist
	for _, resource := range authRelatedResources {
		if err := cleanupResourceIgnoringMissing(t, tc.TestContext, resource.nn, resource.gvk, true); err != nil {
			t.Logf("Error cleaning up resource %s/%s: %v", resource.gvk.Kind, resource.nn.Name, err)
		}
	}

	// Cleanup DataScienceCluster and DSCInitialization
	cleanupListResources(t, tc.TestContext, gvk.DataScienceCluster, "DataScienceCluster")
	cleanupListResources(t, tc.TestContext, gvk.DSCInitialization, "DSCInitialization")
}

// VerifyRequiredOperatorsInstalled ensures that Serverless and ServiceMesh operators are installed.
func (tc *KserveAuthorinoTestCtx) VerifyRequiredOperatorsInstalled(t *testing.T) {
	t.Helper()

	// Verify Service Mesh operator is installed
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Subscription, types.NamespacedName{
			Namespace: "openshift-operators",
			Name:      serviceMeshOpName,
		}),
		WithCustomErrorMsg("Service Mesh operator should be installed for this test"),
	)

	// Verify Serverless operator is installed
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Subscription, types.NamespacedName{
			Namespace: "openshift-serverless",
			Name:      serverlessOpName,
		}),
		WithCustomErrorMsg("Serverless operator should be installed for this test"),
	)
}

// SetupDSCIWithServiceMesh sets up DSCInitialization with ServiceMesh managed.
func (tc *KserveAuthorinoTestCtx) SetupDSCIWithServiceMesh(t *testing.T) {
	t.Helper()

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(CreateDSCI(tc.DSCInitializationNamespacedName.Name, tc.AppsNamespace, tc.MonitoringNamespace)),
		WithCondition(jq.Match(`.status.phase == "%s"`, status.ConditionTypeReady)),
		WithCustomErrorMsg("Failed to create DSCInitialization with ServiceMesh managed"),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	)

	// Verify ServiceMesh is configured as Managed
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithCondition(jq.Match(`.spec.serviceMesh.managementState == "%s"`, operatorv1.Managed)),
		WithCustomErrorMsg("ServiceMesh should be managed in DSCInitialization"),
	)
}

// SetupKServeServerlessMode sets up KServe with Serverless deployment mode.
func (tc *KserveAuthorinoTestCtx) SetupKServeServerlessMode(t *testing.T) {
	t.Helper()

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(CreateDSC(tc.DataScienceClusterNamespacedName.Name)),
		WithMutateFunc(
			testf.TransformPipeline(
				testf.Transform(`.spec.components.kserve.managementState = "%s"`, operatorv1.Managed),
				testf.Transform(`.spec.components.kserve.defaultDeploymentMode = "%s"`, componentApi.Serverless),
				testf.Transform(`.spec.components.kserve.serving.managementState = "%s"`, operatorv1.Managed),
			),
		),
		WithCondition(jq.Match(`.status.phase == "%s"`, status.ConditionTypeReady)),
		WithCustomErrorMsg("Failed to create DataScienceCluster with KServe Serverless mode"),
		WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	)

	// Verify KServe configuration
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(`.spec.components.kserve.defaultDeploymentMode == "%s"`, componentApi.Serverless)),
		WithCustomErrorMsg("KServe should be configured with Serverless deployment mode"),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(`.spec.components.kserve.serving.managementState == "%s"`, operatorv1.Managed)),
		WithCustomErrorMsg("KServe serving should be managed"),
	)
}

// SetupKServeRawMode sets up KServe with Raw deployment mode.
func (tc *KserveAuthorinoTestCtx) SetupKServeRawMode(t *testing.T) {
	t.Helper()

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(
			testf.TransformPipeline(
				testf.Transform(`.spec.components.kserve.defaultDeploymentMode = "%s"`, componentApi.RawDeployment),
				testf.Transform(`.spec.components.kserve.serving.managementState = "%s"`, operatorv1.Managed),
			),
		),
		WithCondition(jq.Match(`.status.phase == "%s"`, status.ConditionTypeReady)),
		WithCustomErrorMsg("Failed to update DataScienceCluster with KServe Raw mode"),
		WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	)

	// Verify KServe configuration
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(`.spec.components.kserve.defaultDeploymentMode == "%s"`, componentApi.RawDeployment)),
		WithCustomErrorMsg("KServe should be configured with Raw deployment mode"),
	)
}

// VerifyAuthorinoNotInstalled ensures that Authorino is not installed in the cluster.
func (tc *KserveAuthorinoTestCtx) VerifyAuthorinoNotInstalled(t *testing.T) {
	t.Helper()

	// Check that Authorino subscription does not exist
	tc.EnsureResourceDoesNotExist(
		WithMinimalObject(gvk.Subscription, types.NamespacedName{
			Namespace: "openshift-operators",
			Name:      authorinoOpName,
		}),
		WithCustomErrorMsg("Authorino subscription should not exist for this test"),
	)

	tc.EnsureResourceDoesNotExist(
		WithMinimalObject(gvk.Subscription, types.NamespacedName{
			Namespace: tc.OperatorNamespace,
			Name:      authorinoOpName,
		}),
		WithCustomErrorMsg("Authorino subscription should not exist in operator namespace for this test"),
	)
}

// VerifyKServeReady verifies that KServe is in Ready state.
func (tc *KserveAuthorinoTestCtx) VerifyKServeReady(t *testing.T) {
	t.Helper()

	// Increase timeout for KServe readiness check
	reset := tc.OverrideEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout, tc.TestTimeouts.defaultEventuallyPollInterval)
	defer reset()

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Kserve, types.NamespacedName{Name: componentApi.KserveInstanceName}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, "True")),
		WithCustomErrorMsg("KServe should be Ready"),
	)
}

// ValidateAuthResourcesNotCreatedServerless verifies that auth-related resources are not created
// when Authorino is not installed in Serverless mode.
func (tc *KserveAuthorinoTestCtx) ValidateAuthResourcesNotCreatedServerless(t *testing.T) {
	t.Helper()

	tc.validateAuthResourcesNotCreated(t, "Serverless")
}

// ValidateAuthResourcesNotCreatedRaw verifies that auth-related resources are not created
// when Authorino is not installed in Raw deployment mode.
func (tc *KserveAuthorinoTestCtx) ValidateAuthResourcesNotCreatedRaw(t *testing.T) {
	t.Helper()

	tc.validateAuthResourcesNotCreated(t, "Raw")
}

// validateAuthResourcesNotCreated is a helper function that validates auth resources are not created.
func (tc *KserveAuthorinoTestCtx) validateAuthResourcesNotCreated(t *testing.T, mode string) {
	t.Helper()

	// Check that EnvoyFilters and AuthorizationPolicies are not created
	for _, resource := range authRelatedResources {
		tc.EnsureResourceDoesNotExist(
			WithMinimalObject(resource.gvk, resource.nn),
			WithCustomErrorMsg(
				"Auth resource %s/%s should not exist when Authorino is not installed in %s mode (RHOAI 2.19.0 regression)",
				resource.gvk.Kind,
				resource.nn.Name,
				mode,
			),
		)
	}

	// Wait and check again for consistency
	time.Sleep(10 * time.Second)
	for _, resource := range authRelatedResources {
		tc.EnsureResourceDoesNotExist(
			WithMinimalObject(resource.gvk, resource.nn),
			WithCustomErrorMsg(
				"Auth resource %s/%s should consistently not exist when Authorino is not installed in %s mode",
				resource.gvk.Kind,
				resource.nn.Name,
				mode,
			),
		)
	}

	t.Logf("SUCCESS: No auth-related resources were created when Authorino is not installed in %s mode", mode)
}
