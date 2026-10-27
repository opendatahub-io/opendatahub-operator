package e2e_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
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
		{"Validate CRDs owned by the operator", operatorTestCtx.ValidateOwnedCRDs},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

func (tc *OperatorTestCtx) filterCRDTestCases(crds []struct {
	name string
	crd  string
}) []struct {
	name string
	crd  string
} {
	platform := tc.FetchPlatformRelease()
	if platform != cluster.XKS {
		return crds
	}

	// In XKS platform (KinD cluster), only a subset of CRDs is expected.
	allowedCRDs := []string{
		"kserves.components.platform.opendatahub.io",
		"platforms.config.opendatahub.io",
	}

	var filtered []struct {
		name string
		crd  string
	}
	for _, c := range crds {
		if slices.Contains(allowedCRDs, c.crd) {
			filtered = append(filtered, c)
		}
	}
	return filtered
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
		{"Kueue CRD", "kueues.components.platform.opendatahub.io"},
		{"Auth CRD", "auths.services.platform.opendatahub.io"},
		{"Platform CRD", "platforms.config.opendatahub.io"},
	}

	operatorPackage := "opendatahub-operator"
	switch tc.FetchPlatformRelease() {
	case cluster.SelfManagedRhoai, cluster.ManagedRhoai:
		operatorPackage = "rhods-operator"
	}
	installedWithOLM, err := tc.CheckOperatorExists(operatorPackage)
	require.NoError(t, err, "Failed to check OLM installation for %s", operatorPackage)
	if installedWithOLM {
		// External module CRDs are installed by the bundle; local make install only installs in-tree CRDs.
		crdsTestCases = append(crdsTestCases, []struct {
			name string
			crd  string
		}{
			{"AI Gateway CRD", "aigateways.components.platform.opendatahub.io"},
			{"AI Hub CRD", "aihubs.components.platform.opendatahub.io"},
			{"AI Pipelines CRD", "aipipelines.components.platform.opendatahub.io"},
			{"Dashboard CRD", "dashboards.components.platform.opendatahub.io"},
			{"Feast Operator CRD", "feastoperators.components.platform.opendatahub.io"},
			{"KServe CRD", "kserves.components.platform.opendatahub.io"},
			{"MCP Lifecycle Operator CRD", "mcplifecycleoperators.components.platform.opendatahub.io"},
			{"MLflow Operator CRD", "mlflowoperators.components.platform.opendatahub.io"},
			{"OGX CRD", "ogxs.components.platform.opendatahub.io"},
			{"Ray CRD", "rays.components.platform.opendatahub.io"},
			{"Spark Operator CRD", "sparkoperators.components.platform.opendatahub.io"},
			{"Trainer CRD", "trainers.components.platform.opendatahub.io"},
			{"TrustyAI CRD", "trustyais.components.platform.opendatahub.io"},
			{"Workbenches CRD", "workbenches.components.platform.opendatahub.io"},
		}...)
	}

	crdsTestCases = tc.filterCRDTestCases(crdsTestCases)

	for _, testCase := range crdsTestCases {
		t.Run("Validate "+testCase.name, func(t *testing.T) {
			t.Parallel()
			tc.EnsureCRDEstablished(testCase.crd)
		})
	}
}
