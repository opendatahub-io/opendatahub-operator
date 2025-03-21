package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

type DeletionTestCtx struct {
	*TestContext
}

// deletionTestSuite runs the deletion test suite.
func deletionTestSuite(t *testing.T) {
	t.Helper()

	// Initialize the test context.
	tc, err := NewTestContext(t)
	require.NoError(t, err, "Failed to initialize test context")

	// Create an instance of test context.
	deletionTestCtx := DeletionTestCtx{
		TestContext: tc,
	}

	// Define the test cases
	testCases := []TestCase{
		{"Deletion DataScienceCluster instance", deletionTestCtx.DeletionDSC},
		{"Deletion DSCInitialization instance", deletionTestCtx.DeletionDSCI},
	}

	// Run the test suite.
	deletionTestCtx.RunTestCases(t, testCases)
}

// DeletionDSC deletes the DataScienceCluster instance if it exists.
func (tc *DeletionTestCtx) DeletionDSC(t *testing.T) {
	t.Helper()

	// Delete the DataScienceCluster instance
	tc.DeleteResource(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName)
}

// DeletionDSCI deletes the DSCInitialization instance if it exists.
func (tc *DeletionTestCtx) DeletionDSCI(t *testing.T) {
	t.Helper()

	// Delete the DSCInitialization instance
	tc.DeleteResource(gvk.DSCInitialization, tc.DSCInitializationNamespacedName)
}
