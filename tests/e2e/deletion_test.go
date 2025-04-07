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
		{"Delete DataScienceCluster instance", deletionTestCtx.TestDSCDeletion},
		{"Delete DSCInitialization instance", deletionTestCtx.TestDSCIDeletion},
	}

	// Run the test suite.
	deletionTestCtx.RunTestCases(t, testCases)
}

// TestDSCDeletion deletes the DataScienceCluster instance if it exists.
func (tc *DeletionTestCtx) TestDSCDeletion(t *testing.T) {
	t.Helper()

	// Delete the DataScienceCluster instance
	tc.DeleteResource(WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName))
}

// TestDSCIDeletion deletes the DSCInitialization instance if it exists.
func (tc *DeletionTestCtx) TestDSCIDeletion(t *testing.T) {
	t.Helper()

	// Delete the DSCInitialization instance
	tc.DeleteResource(WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName))
}
