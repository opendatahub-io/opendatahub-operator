package e2e_test

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	gTypes "github.com/onsi/gomega/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	modelregistryctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const (
	testNamespace               = "test-model-registries"   // Namespace used for model registry testing
	dsciInstanceNameDuplicate   = "e2e-test-dsci-duplicate" // Instance name for the duplicate DSCInitialization resource
	dscInstanceNameDuplicate    = "e2e-test-dsc-duplicate"  // Instance name for the duplicate DataScienceCluster resource
	openshiftOperatorsNamespace = "openshift-operators"     // Namespace for OpenShift Operators
	serverlessOperatorNamespace = "openshift-serverless"    // Namespace for the Serverless Operator
)

// DSCTestCtx holds the context for the DSCInitialization and DataScienceCluster management tests.
type DSCTestCtx struct {
	*TestContext
}

// dscManagementTestSuite runs the DataScienceCluster and DSCInitialization management test suite.
func dscManagementTestSuite(t *testing.T) {
	t.Helper()

	// Initialize the test context.
	tc, err := NewTestContext(t)
	require.NoError(t, err, "Failed to initialize test context")

	// Create an instance of test context.
	dscTestCtx := DSCTestCtx{
		TestContext: tc,
	}

	// Define test cases.
	testCases := []TestCase{
		{"Ensure Service Mesh , Serverless and Observability operators are installed", dscTestCtx.ValidateOperatorsInstallation},
		{"Validate creation of DSCInitialization instance", dscTestCtx.ValidateDSCICreation},
		{"Validate creation of DataScienceCluster instance", dscTestCtx.ValidateDSCCreation},
		{"Validate ServiceMeshSpec in DSCInitialization instance", dscTestCtx.ValidateServiceMeshSpecInDSCI},
		//TODO: disabled until RHOAIENG-29225 is resolved
		// {"Validate ServiceMeshControlPlane exists and is recreated upon deletion.", dscTestCtx.ValidateServiceMeshControlPlane},
		{"Validate Knative resource", dscTestCtx.ValidateKnativeSpecInDSC},
		{"Validate owned namespaces exist", dscTestCtx.ValidateOwnedNamespacesAllExist},
		{"Validate default NetworkPolicy exist", dscTestCtx.ValidateDefaultNetworkPolicyExists},
		{"Validate Observability operators are installed", dscTestCtx.ValidateObservabilityOperatorsInstallation},
		{"Validate components deployment failure", dscTestCtx.ValidateComponentsDeploymentFailure},
	}

	// Append webhook-specific tests.
	if testOpts.webhookTest {
		webhookTests := []TestCase{
			{"Validate creation of more than one DSCInitialization instance", dscTestCtx.ValidateDSCIDuplication},
			{"Validate creation of more than one DataScienceCluster instance", dscTestCtx.ValidateDSCDuplication},
			{"Validate Model Registry Configuration Changes", dscTestCtx.ValidateModelRegistryConfig},
		}

		testCases = append(testCases, TestCase{
			name: "Webhook",
			testFn: func(t *testing.T) {
				t.Helper()
				RunTestCases(t, webhookTests)
			},
		})
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateOperatorsInstallation ensures the Service Mesh and Serverless operators are installed.
func (tc *DSCTestCtx) ValidateOperatorsInstallation(t *testing.T) {
	t.Helper()

	// Define operators to be installed.
	operators := []struct {
		nn                types.NamespacedName
		skipOperatorGroup bool
	}{
		{nn: types.NamespacedName{Name: serviceMeshOpName, Namespace: openshiftOperatorsNamespace}, skipOperatorGroup: true},
		{nn: types.NamespacedName{Name: serverlessOpName, Namespace: serverlessOperatorNamespace}, skipOperatorGroup: false},
		{nn: types.NamespacedName{Name: authorinoOpName, Namespace: openshiftOperatorsNamespace}, skipOperatorGroup: true},
		{nn: types.NamespacedName{Name: observabilityOpName, Namespace: observabilityOpNamespace}, skipOperatorGroup: false},
		{nn: types.NamespacedName{Name: telemetryOpName, Namespace: telemetryOpNamespace}, skipOperatorGroup: false},
		{nn: types.NamespacedName{Name: tempoOpName, Namespace: tempoOpNamespace}, skipOperatorGroup: false},
		{nn: types.NamespacedName{Name: opentelemetryOpName, Namespace: opentelemetryOpNamespace}, skipOperatorGroup: false},
	}

	// Create and run test cases in parallel.
	testCases := make([]TestCase, len(operators))
	for i, op := range operators {
		testCases[i] = TestCase{
			name: fmt.Sprintf("Ensure %s is installed", op.nn.Name),
			testFn: func(t *testing.T) {
				t.Helper()
				tc.EnsureOperatorInstalled(op.nn, op.skipOperatorGroup)
			},
		}
	}

	RunTestCases(t, testCases, WithParallel())
}

func (tc *DSCTestCtx) ValidateObservabilityOperatorsInstallation(t *testing.T) {
	t.Helper()

	// Define operators to be installed.
	operators := []struct {
		nn                types.NamespacedName
		skipOperatorGroup bool
	}{
		{nn: types.NamespacedName{Name: telemetryOpName, Namespace: telemetryOpNamespace}, skipOperatorGroup: false},
		{nn: types.NamespacedName{Name: observabilityOpName, Namespace: observabilityOpNamespace}, skipOperatorGroup: false},
	}

	// Create and run test cases in parallel.
	testCases := make([]TestCase, len(operators))
	for i, op := range operators {
		testCases[i] = TestCase{
			name: fmt.Sprintf("Ensure %s is installed", op.nn.Name),
			testFn: func(t *testing.T) {
				t.Helper()
				tc.EnsureOperatorInstalled(op.nn, op.skipOperatorGroup)
			},
		}
	}
	RunTestCases(t, testCases, WithParallel())
}

// ValidateDSCICreation validates the creation of a DSCInitialization.
func (tc *DSCTestCtx) ValidateDSCICreation(t *testing.T) {
	t.Helper()

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(CreateDSCI(tc.DSCInitializationNamespacedName.Name, tc.AppsNamespace, tc.MonitoringNamespace)),
		WithCondition(jq.Match(`.status.phase == "%s"`, status.ConditionTypeReady)),
		WithCustomErrorMsg("Failed to create DSCInitialization resource %s", tc.DSCInitializationNamespacedName.Name),

		// Increase time required to get DSCI created
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	)
}

// ValidateDSCCreation validates the creation of a DataScienceCluster.
func (tc *DSCTestCtx) ValidateDSCCreation(t *testing.T) {
	t.Helper()

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(CreateDSC(tc.DataScienceClusterNamespacedName.Name)),
		WithCondition(jq.Match(`.status.phase == "%s"`, status.ConditionTypeReady)),
		WithCustomErrorMsg("Failed to create DataScienceCluster resource %s", tc.DataScienceClusterNamespacedName.Name),

		// Increase time required to get DSC created
		WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	)
}

// ValidateServiceMeshSpecInDSCI validates the ServiceMeshSpec within a DSCInitialization instance.
func (tc *DSCTestCtx) ValidateServiceMeshSpecInDSCI(t *testing.T) {
	t.Helper()

	// expected ServiceMeshSpec.
	expServiceMeshSpec := &infrav1.ServiceMeshSpec{
		ManagementState: operatorv1.Managed,
		ControlPlane: infrav1.ControlPlaneSpec{
			Name:              serviceMeshControlPlane,
			Namespace:         serviceMeshNamespace,
			MetricsCollection: serviceMeshMetricsCollection,
		},
		Auth: infrav1.AuthSpec{
			Audiences: []string{"https://kubernetes.default.svc"},
		},
	}

	// Marshal the expected ServiceMeshSpec to JSON.
	expServiceMeshSpecJSON, err := json.Marshal(expServiceMeshSpec)
	tc.g.Expect(err).ShouldNot(HaveOccurred(), "Error marshaling expected ServiceMeshSpec")

	// Assert that the actual ServiceMeshSpec matches the expected one.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithCondition(jq.Match(`.spec.serviceMesh == %s`, expServiceMeshSpecJSON)),
		WithCustomErrorMsg("Error validating DSCInitialization instance: Service Mesh spec mismatch"),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithCondition(jq.Match(`.status.phase == "Ready"`)))
}

// ValidateServiceMeshControlPlane checks that ServiceMeshControlPlane exists and is recreated upon deletion.
func (tc *DSCTestCtx) ValidateServiceMeshControlPlane(t *testing.T) {
	t.Helper()

	smcp := types.NamespacedName{Name: serviceMeshControlPlane, Namespace: serviceMeshNamespace}

	// Ensure service mesh feature tracker is in phase ready
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.FeatureTracker, types.NamespacedName{Name: "opendatahub-mesh-control-plane-creation"}),
		WithCondition(jq.Match(`.status.phase == "Ready"`)))

	// Check ServiceMeshControlPlane was created.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ServiceMeshControlPlane, smcp),
	)

	// Delete it.
	tc.DeleteResource(
		WithMinimalObject(gvk.ServiceMeshControlPlane, smcp),
		WithWaitForDeletion(true),
	)

	// Check eventually got recreated.
	tc.EnsureResourceExistsConsistently(
		WithMinimalObject(gvk.ServiceMeshControlPlane, smcp),
	)
}

// ValidateKnativeSpecInDSC validates that the Kserve serving spec in the DataScienceCluster matches the expected spec.
func (tc *DSCTestCtx) ValidateKnativeSpecInDSC(t *testing.T) {
	t.Helper()

	// expected ServingSpec
	expServingSpec := &infrav1.ServingSpec{
		ManagementState: operatorv1.Managed,
		Name:            knativeServingNamespace,
		IngressGateway: infrav1.GatewaySpec{
			Certificate: infrav1.CertificateSpec{
				Type: infrav1.OpenshiftDefaultIngress,
			},
		},
	}

	// Marshal the expected ServingSpec to JSON
	expServingSpecJSON, err := json.Marshal(expServingSpec)
	tc.g.Expect(err).ShouldNot(HaveOccurred(), "Error marshaling expected ServingSpec")

	// Assert that the actual ServingSpec matches the expected one.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(`.spec.components.kserve.serving == %s`, expServingSpecJSON)),
		WithCustomErrorMsg("Error validating DSCInitialization instance: Knative Serving spec mismatch"),
	)
}

// ValidateOwnedNamespacesAllExist verifies that the owned namespaces exist.
func (tc *DSCTestCtx) ValidateOwnedNamespacesAllExist(t *testing.T) {
	t.Helper()

	// Ensure namespaces with the owned namespace label exist.
	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{}),
		WithListOptions(
			&client.ListOptions{
				LabelSelector: k8slabels.SelectorFromSet(
					k8slabels.Set{labels.ODH.OwnedNamespace: "true"},
				),
			}),
		WithCondition(BeNumerically(">=", ownedNamespaceNumber)),
		WithCustomErrorMsg("Expected at least %d owned namespaces with label '%s'.", ownedNamespaceNumber, labels.ODH.OwnedNamespace),
	)
}

// ValidateDefaultNetworkPolicyExists verifies that the default network policy exists.
func (tc *DSCTestCtx) ValidateDefaultNetworkPolicyExists(t *testing.T) {
	t.Helper()

	dsci := tc.FetchDSCInitialization()

	// Ensure namespaces with the owned namespace label exist.
	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.NetworkPolicy, types.NamespacedName{Namespace: dsci.Spec.ApplicationsNamespace, Name: dsci.Spec.ApplicationsNamespace}),
		WithCustomErrorMsg("Expected the default NetworkPolicy to be created."),
	)
}

// ValidateDSCIDuplication ensures that no duplicate DSCInitialization resource can be created.
func (tc *DSCTestCtx) ValidateDSCIDuplication(t *testing.T) {
	t.Helper()

	dup := CreateDSCI(dsciInstanceNameDuplicate, tc.AppsNamespace, tc.MonitoringNamespace)
	tc.EnsureResourceIsUnique(dup)
}

// ValidateDSCDuplication ensures that no duplicate DataScienceCluster resource can be created.
func (tc *DSCTestCtx) ValidateDSCDuplication(t *testing.T) {
	t.Helper()

	dup := CreateDSC(dscInstanceNameDuplicate)
	tc.EnsureResourceIsUnique(dup, "Error validating DataScienceCluster duplication")
}

// ValidateModelRegistryConfig validates the ModelRegistry configuration changes based on ManagementState.
func (tc *DSCTestCtx) ValidateModelRegistryConfig(t *testing.T) {
	t.Helper()

	// Retrieve the DataScienceCluster object.
	dsc := tc.FetchDataScienceCluster()

	// Check if the ModelRegistry is managed.
	if dsc.Spec.Components.ModelRegistry.ManagementState == operatorv1.Managed {
		// Ensure changing registriesNamespace is not allowed and expect failure.
		tc.UpdateRegistriesNamespace(testNamespace, modelregistryctrl.DefaultModelRegistriesNamespace, true)

		// No further checks if it's managed
		return
	}

	// Ensure setting registriesNamespace to a non-default value is allowed.
	// No error is expected, and we check the value of the patch after it's successful.
	tc.UpdateRegistriesNamespace(testNamespace, testNamespace, false)

	// Ensure resetting registriesNamespace to the default value is allowed.
	tc.UpdateRegistriesNamespace(modelregistryctrl.DefaultModelRegistriesNamespace, modelregistryctrl.DefaultModelRegistriesNamespace, false)
}

// UpdateRegistriesNamespace updates the ModelRegistry component's `RegistriesNamespace` field.
func (tc *DSCTestCtx) UpdateRegistriesNamespace(targetNamespace, expectedValue string, shouldFail bool) {
	// Build the condition:
	// If shouldFail, we expect a failure (Not(Succeed())).
	// If should not fail, we expect the registriesNamespace to match the expected value.
	var expectedCondition gTypes.GomegaMatcher
	if shouldFail {
		expectedCondition = Not(Succeed()) // If shouldFail is true, expect failure.
	} else {
		expectedCondition = And(Succeed(), jq.Match(`.spec.components.modelregistry.registriesNamespace == "%s"`, expectedValue))
	}

	// Update the registriesNamespace field.
	tc.EnsureResourceCreatedOrPatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.modelregistry.registriesNamespace = "%s"`, targetNamespace)),
		WithCondition(expectedCondition),
		WithCustomErrorMsg("Failed to update RegistriesNamespace to %s, expected %s", targetNamespace, expectedValue),
	)
}

// ValidateComponentsDeploymentFailure simulates component deployment failure using restrictive resource quota.
func (tc *DSCTestCtx) ValidateComponentsDeploymentFailure(t *testing.T) {
	t.Helper()

	// To handle upstream/downstream i trimmed prefix(odh) from few controller names
	componentToControllerMap := map[string]string{
		componentApi.CodeFlareComponentName:            "codeflare-operator-manager",
		componentApi.DashboardComponentName:            "dashboard",
		componentApi.DataSciencePipelinesComponentName: "data-science-pipelines-operator-controller-manager",
		componentApi.FeastOperatorComponentName:        "feast-operator-controller-manager",
		componentApi.KserveComponentName:               "kserve-controller-manager",
		componentApi.KueueComponentName:                "kueue-controller-manager",
		componentApi.LlamaStackOperatorComponentName:   "llama-stack-k8s-operator-controller-manager",
		componentApi.ModelMeshServingComponentName:     "modelmesh-controller",
		componentApi.ModelRegistryComponentName:        "model-registry-operator-controller-manager",
		componentApi.RayComponentName:                  "kuberay-operator",
		componentApi.TrainingOperatorComponentName:     "kubeflow-training-operator",
		componentApi.TrustyAIComponentName:             "trustyai-service-operator-controller-manager",
		componentApi.WorkbenchesComponentName:          "notebook-controller-manager",
	}

	// Error message includes components + internal components name
	var internalComponentToControllerMap = map[string]string{
		componentApi.ModelControllerComponentName: "model-controller",
	}

	components := slices.Collect(maps.Keys(componentToControllerMap))
	componentsLength := len(components)

	t.Log("Verifying component count matches DSC Components struct")
	expectedComponentCount := reflect.TypeOf(dscv1.Components{}).NumField()
	tc.g.Expect(componentsLength).Should(Equal(expectedComponentCount),
		"allComponents list is out of sync with DSC Components struct. "+
			"Expected %d components but found %d. "+
			"Please update the allComponents list when adding new components.",
		expectedComponentCount, componentsLength)

	restrictiveQuota := createRestrictiveQuotaForOperator(tc.AppsNamespace)

	t.Log("Creating restrictive resource quota in operator namespace")
	tc.EnsureResourceCreatedOrPatched(
		WithObjectToCreate(restrictiveQuota),
	)

	// important: register cleanup immediately after creation to avoid flakiness in local testing
	t.Cleanup(func() {
		t.Log("Cleaning up restrictive quota")
		tc.DeleteResource(WithObjectToCreate(restrictiveQuota))
	})

	t.Log("Enabling all components in DataScienceCluster")
	tc.EnsureResourceCreatedOrPatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(updateAllComponentsTransform(components, operatorv1.Managed)),
	)

	t.Log("Verifying component deployments are stuck due to quota")
	allControllers := slices.Concat(
		slices.Collect(maps.Values(componentToControllerMap)),
		slices.Collect(maps.Values(internalComponentToControllerMap)),
	)
	tc.verifyDeploymentsStuckDueToQuota(t, allControllers)

	t.Log("Verifying DSC reports all failed components")
	allComponents := slices.Concat(
		components,
		slices.Collect(maps.Keys(internalComponentToControllerMap)),
	)
	sort.Strings(allComponents)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(
			jq.Match(
				`.status.conditions[]
				| select(.type == "ComponentsReady" and .status == "False")
				| .message == "%s"`,
				"Some components are not ready: "+strings.Join(allComponents, ","),
			),
		),
	)

	t.Log("Disabling all components and verifying no managed components are reported")
	tc.EnsureResourceCreatedOrPatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(updateAllComponentsTransform(components, operatorv1.Removed)),
	)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(
			`.status.conditions[]
			| select(.type == "ComponentsReady" and .status == "%s")
			| .message
			| test("nomanagedcomponents"; "i")`,
			metav1.ConditionTrue,
		)),
	)
}

// enable/disable all components.
func updateAllComponentsTransform(components []string, state operatorv1.ManagementState) testf.TransformFn {
	transformParts := make([]string, len(components))
	for i, component := range components {
		transformParts[i] = fmt.Sprintf(`.spec.components.%s.managementState = "%s"`, component, state)
	}

	return testf.Transform("%s", strings.Join(transformParts, " | "))
}

func createRestrictiveQuotaForOperator(namespace string) *corev1.ResourceQuota {
	return &corev1.ResourceQuota{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ResourceQuota",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-restrictive-quota",
			Namespace: namespace,
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceRequestsCPU:    resource.MustParse("0.1m"),
				corev1.ResourceRequestsMemory: resource.MustParse("1Ki"),
				corev1.ResourceLimitsCPU:      resource.MustParse("0.1m"),
				corev1.ResourceLimitsMemory:   resource.MustParse("1Ki"),
			},
		},
	}
}

func (tc *DSCTestCtx) verifyDeploymentsStuckDueToQuota(t *testing.T, allControllers []string) {
	t.Helper()

	expectedCount := len(allControllers)

	tc.EnsureResourcesExist(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{Namespace: tc.AppsNamespace}),
		WithCondition(jq.Match("%s", fmt.Sprintf(`
			map(
				select(.metadata.name | test("%s"; "i")) |
				select(
					.status.conditions[]? |
					select(.type == "ReplicaFailure") |
					select(.status == "True") |
					select(.message | test(
						"forbidden: exceeded quota: test-restrictive-quota|" +
						"forbidden: failed quota: test-restrictive-quota|" +
						"forbidden"; "i"
					))
				)
			) |
			length == %d
		`, strings.Join(allControllers, "|"), expectedCount))),
		WithCustomErrorMsg(fmt.Sprintf("Expected all %d component deployments to have quota error messages", expectedCount)),
		WithEventuallyTimeout(tc.TestTimeouts.longEventuallyTimeout),
	)
}
