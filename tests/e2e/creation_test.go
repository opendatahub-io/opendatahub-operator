package e2e_test

import (
	"testing"

	gTypes "github.com/onsi/gomega/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	modelregistryctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const (
	testNamespace             = "test-model-registries"  // Namespace used for model registry testing
	dsciInstanceNameDuplicate = "default-dsci-duplicate" // Instance name for the duplicate DSCInitialization resource
	dscInstanceNameDuplicate  = "default-dsc-duplicate"  // Instance name for the duplicate DataScienceCluster resource
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
		{"Ensure required operators are installed", dscTestCtx.ValidateOperatorsInstallation},
		{"Ensure required resources are created", dscTestCtx.ValidateResourcesCreation},
		{"Validate creation of DSCInitialization instance", dscTestCtx.ValidateDSCICreation},
		{"Validate creation of DataScienceCluster instance", dscTestCtx.ValidateDSCCreation},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// dscValidationTestSuite runs the DataScienceCluster and DSCInitialization validation test suite.
func dscValidationTestSuite(t *testing.T) {
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
		{"Validate HardwareProfile resource", dscTestCtx.ValidateHardwareProfileCR},
		{"Validate owned namespaces exist", dscTestCtx.ValidateOwnedNamespacesAllExist},
		{"Validate default NetworkPolicy exist", dscTestCtx.ValidateDefaultNetworkPolicyExists},
	}

	// Run the test suite.
	RunTestCases(t, testCases, WithParallel())
}

func dscWebhookTestSuite(t *testing.T) {
	t.Helper()

	// Initialize the test context.
	tc, err := NewTestContext(t)
	require.NoError(t, err, "Failed to initialize test context")

	// Create an instance of test context.
	dscTestCtx := DSCTestCtx{
		TestContext: tc,
	}

	// Define dsci/dsc webhook-specific tests.
	webhookTests := []TestCase{
		{"Validate creation of more than one DSCInitialization instance", dscTestCtx.ValidateDSCIDuplication},
		{"Validate creation of more than one DataScienceCluster instance", dscTestCtx.ValidateDSCDuplication},
		{"Validate Model Registry Configuration Changes", dscTestCtx.ValidateModelRegistryConfig},
	}

	RunTestCases(t, webhookTests, WithParallel())
}

// ValidateOperatorsInstallation ensures the required operators are installed.
func (tc *DSCTestCtx) ValidateOperatorsInstallation(t *testing.T) {
	t.Helper()

	skipUnless(t, []TestTag{Smoke})

	// Define operators to be installed.
	operators := []Operator{
		{nn: types.NamespacedName{Name: certManagerOpName, Namespace: certManagerOpNamespace}, skipOperatorGroup: false, globalOperatorGroup: true, channel: certManagerOpChannel},
		{nn: types.NamespacedName{Name: kuadrantOpName, Namespace: kuadrantNamespace}, skipOperatorGroup: false, globalOperatorGroup: true, channel: defaultOperatorChannel},
		{nn: types.NamespacedName{Name: leaderWorkerSetOpName, Namespace: leaderWorkerSetNamespace}, skipOperatorGroup: false, globalOperatorGroup: false, channel: leaderWorkerSetChannel}, //nolint:lll
		{nn: types.NamespacedName{Name: jobSetOpName, Namespace: jobSetOpNamespace}, skipOperatorGroup: false, globalOperatorGroup: false, channel: jobSetOpChannel},
	}

	tc.ensureOperatorsAreInstalled(t, operators)
}

// ValidateResourcesCreation validates the creation of the required resources.
func (tc *DSCTestCtx) ValidateResourcesCreation(t *testing.T) {
	t.Helper()

	skipUnless(t, []TestTag{Smoke})

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(CreateJobSetOperator()),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "Available") | .status == "True"`)),
		WithCustomErrorMsg("Failed to create JobSetOperator resource"),
	)
}

// ValidateDSCICreation validates the creation of a DSCInitialization.
func (tc *DSCTestCtx) ValidateDSCICreation(t *testing.T) {
	t.Helper()

	skipUnless(t, []TestTag{Smoke})

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

	skipUnless(t, []TestTag{Smoke})

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(CreateDSC(tc.DataScienceClusterNamespacedName.Name, tc.WorkbenchesNamespace)),
		WithCondition(jq.Match(`.status.phase == "%s"`, status.ConditionTypeReady)),
		WithCustomErrorMsg("Failed to create DataScienceCluster resource %s", tc.DataScienceClusterNamespacedName.Name),

		// Increase time required to get DSC created
		WithEventuallyTimeout(tc.TestTimeouts.mediumEventuallyTimeout),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	)
}

// ValidateOwnedNamespacesAllExist verifies that the owned namespaces exist.
func (tc *DSCTestCtx) ValidateOwnedNamespacesAllExist(t *testing.T) {
	t.Helper()

	skipUnless(t, []TestTag{Smoke})

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

	skipUnless(t, []TestTag{Smoke})

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

	skipUnless(t, []TestTag{Smoke})

	dup := CreateDSCI(dsciInstanceNameDuplicate, tc.AppsNamespace, tc.MonitoringNamespace)
	tc.EnsureResourceIsUnique(dup, "Error validating DSCInitialization duplication")

	dupv1 := CreateDSCIv1(dsciInstanceNameDuplicate, tc.AppsNamespace, tc.MonitoringNamespace)
	tc.EnsureResourceIsUnique(dupv1, "Error validating DSCInitialization duplication v1")
}

// ValidateDSCDuplication ensures that no duplicate DataScienceCluster resource can be created.
func (tc *DSCTestCtx) ValidateDSCDuplication(t *testing.T) {
	t.Helper()

	skipUnless(t, []TestTag{Smoke})

	dsc := CreateDSC(dscInstanceNameDuplicate, tc.WorkbenchesNamespace)
	tc.EnsureResourceIsUnique(dsc, "Error validating DataScienceCluster duplication")

	dsv1 := CreateDSCv1(dscInstanceNameDuplicate, tc.WorkbenchesNamespace)
	tc.EnsureResourceIsUnique(dsv1, "Error validating DataScienceCluster duplication v1")
}

// ValidateModelRegistryConfig validates the ModelRegistry configuration changes based on ManagementState.
func (tc *DSCTestCtx) ValidateModelRegistryConfig(t *testing.T) {
	t.Helper()

	skipUnless(t, []TestTag{Tier1})

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
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.modelregistry.registriesNamespace = "%s"`, targetNamespace)),
		WithCondition(expectedCondition),
		WithCustomErrorMsg("Failed to update RegistriesNamespace to %s, expected %s", targetNamespace, expectedValue),
	)
}

func (tc *DSCTestCtx) ValidateHardwareProfileCR(t *testing.T) {
	t.Helper()

	skipUnless(t, []TestTag{Smoke})

	// verifed default hardwareprofile exists and api version is correct on v1.
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.HardwareProfile, types.NamespacedName{Name: "default-profile", Namespace: tc.AppsNamespace}),
		WithCondition(And(
			jq.Match(`.spec.identifiers[0].defaultCount == 2`),
			jq.Match(`.metadata.annotations["opendatahub.io/managed"] == "false"`),
			jq.Match(`.apiVersion == "infrastructure.opendatahub.io/v1"`),
		)),
		WithCustomErrorMsg("Default hardwareprofile should have defaultCount=2, managed=false, and use v1 API version"),
	)

	// update default hardwareprofile to different value and check it is updated.
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.HardwareProfile, types.NamespacedName{Name: "default-profile", Namespace: tc.AppsNamespace}),
		WithMutateFunc(testf.Transform(`
			.spec.identifiers[0].defaultCount = 4 |
			.metadata.annotations["opendatahub.io/managed"] = "false"
		`)),
		WithCondition(And(
			Succeed(),
			jq.Match(`.spec.identifiers[0].defaultCount == 4`),
			jq.Match(`.metadata.annotations["opendatahub.io/managed"] == "false"`),
		)),
		WithCustomErrorMsg("Failed to update defaultCount from 2 to 4"),
	)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.HardwareProfile, types.NamespacedName{Name: "default-profile", Namespace: tc.AppsNamespace}),
		WithCondition(jq.Match(`.spec.identifiers[0].defaultCount == 4`)),
		WithCustomErrorMsg("Should have defaultCount to 4 but now got %s", jq.Match(`.spec.identifiers[0].defaultCount`)),
	)

	// delete default hardwareprofile and check it is recreated with default values.
	tc.DeleteResource(
		WithMinimalObject(gvk.HardwareProfile, types.NamespacedName{Name: "default-profile", Namespace: tc.AppsNamespace}),
	)

	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.HardwareProfile, types.NamespacedName{Name: "default-profile", Namespace: tc.AppsNamespace}),
		WithCondition(And(
			jq.Match(`.spec.identifiers[0].defaultCount == 2`),
			jq.Match(`.metadata.annotations["opendatahub.io/managed"] == "false"`),
		)),
		WithCustomErrorMsg("Hardware profile was not recreated with default values"),
	)
}
