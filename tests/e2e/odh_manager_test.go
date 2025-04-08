package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"
)

type OperatorTestCtx struct {
	*TestContext
}

func odhOperatorTestSuite(t *testing.T) {
	t.Helper()

	// Initialize the test context.
	tc, err := NewTestContext(t)
	require.NoError(t, err, "Failed to initialize test context")

	// Create an instance of test context.
	operatorTestCtx := OperatorTestCtx{
		TestContext: tc,
	}

	// Define test cases.
	testCases := []TestCase{
		{name: "Validate ODH Operator pod", testFn: operatorTestCtx.testODHDeployment},
		{name: "Validate CRDs owned by the operator", testFn: operatorTestCtx.ValidateOwnedCRDs},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// testODHDeployment checks if the ODH deployment exists and is correctly configured.
func (tc *OperatorTestCtx) testODHDeployment(t *testing.T) {
	t.Helper()

	// Verify if the operator deployment is created
	controllerDeployment := "opendatahub-operator-controller-manager"
	tc.EnsureDeploymentReady(types.NamespacedName{Namespace: tc.OperatorNamespace, Name: controllerDeployment}, 1)
}

// ValidateOwnedCRDs validates if the owned CRDs are properly created and available.
func (tc *OperatorTestCtx) ValidateOwnedCRDs(t *testing.T) {
	t.Helper()

	crdsTestCases := []struct {
		name string
		crd  string
	}{
		{"Datascience Cluster CRD", "datascienceclusters.datasciencecluster.opendatahub.io"},
		{"DataScienceCluster Initialization CRD", "dscinitializations.dscinitialization.opendatahub.io"},
		{"FeatureTracker CRD", "featuretrackers.features.opendatahub.io"},
		{"Dashboard CRD", "dashboards.components.platform.opendatahub.io"},
		{"Ray CRD", "rays.components.platform.opendatahub.io"},
		{"ModelRegistry CRD", "modelregistries.components.platform.opendatahub.io"},
		{"TrustyAI CRD", "trustyais.components.platform.opendatahub.io"},
		{"Kueue CRD", "kueues.components.platform.opendatahub.io"},
		{"TrainingOperator CRD", "trainingoperators.components.platform.opendatahub.io"},
		{"FeastOperator CRD", "feastoperators.components.platform.opendatahub.io"},
		{"DataSciencePipelines CRD", "datasciencepipelines.components.platform.opendatahub.io"},
		{"Workbenches CRD", "workbenches.components.platform.opendatahub.io"},
		{"Kserve CRD", "kserves.components.platform.opendatahub.io"},
		{"ModelMeshServing CRD", "modelmeshservings.components.platform.opendatahub.io"},
		{"ModelController CRD", "modelcontrollers.components.platform.opendatahub.io"},
		{"Monitoring CRD", "monitorings.services.platform.opendatahub.io"},
	}

	for _, testCase := range crdsTestCases {
		t.Run("Validate "+testCase.name, func(t *testing.T) {
			t.Parallel()
			tc.EnsureCRDEstablished(testCase.crd)
		})
	}
}
