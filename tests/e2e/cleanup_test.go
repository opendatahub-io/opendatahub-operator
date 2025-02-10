package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

// CleanupAllResources handles the cleanup of all resources (DSC, DSCI, etc.)
func CleanupAllResources(t *testing.T) {
	t.Helper()

	// Initialize the test context.
	tc, err := NewTestContext(t)
	require.NoError(t, err, "Failed to initialize test context")

	// Cleanup DataScienceCluster
	t.Log("Cleaning up DataScienceCluster")
	tc.DeleteResourceIfExists(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName)

	// Cleanup DSCInitialization
	t.Log("Cleaning up DSCInitialization")
	tc.DeleteResourceIfExists(gvk.DSCInitialization, tc.DSCInitializationNamespacedName)
}
