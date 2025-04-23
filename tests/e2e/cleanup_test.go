package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

// cleanupResource logs and deletes the specified resource if it exists.
func cleanupResource(t *testing.T, tc *TestContext, kind schema.GroupVersionKind, name types.NamespacedName, label string) { //nolint:thelper
	t.Logf("Cleaning up %s if present", label)
	tc.DeleteResourceIfExists(WithMinimalObject(kind, name))
}

// cleanupListResources deletes a list of resources of a given kind.
func cleanupListResources(t *testing.T, tc *TestContext, kind schema.GroupVersionKind, label string) { //nolint:thelper
	list := tc.FetchResources(
		WithMinimalObject(kind, types.NamespacedName{}),
		WithListOptions(&client.ListOptions{}),
	)

	if len(list) > 0 {
		t.Logf("Detected %d %s(s), deleting", len(list), label)
		for _, res := range list {
			cleanupResource(t, tc, kind, types.NamespacedName{Name: res.GetName()}, label)
		}
	}
}

// CleanupAllResources handles the cleanup of all resources (DSC, DSCI, etc.)
func CleanupAllResources(t *testing.T) {
	t.Helper()

	// Initialize the test context.
	tc, err := NewTestContext(t)
	require.NoError(t, err, "Failed to initialize test context")

	// Cleanup DataScienceCluster and DSCInitialization
	cleanupResource(t, tc, gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName, "DataScienceCluster")
	cleanupResource(t, tc, gvk.DSCInitialization, tc.DSCInitializationNamespacedName, "DSCInitialization")
}

// CleanupDefaultResources handles the cleanup of default resources: DSC, DSCI, and AuthConfig.
func CleanupDefaultResources(t *testing.T) {
	t.Helper()

	// Initialize the test context.
	tc, err := NewTestContext(t)
	require.NoError(t, err, "Failed to initialize test context")

	// Cleanup existing DataScienceCluster and DSCInitialization.
	cleanupListResources(t, tc, gvk.DataScienceCluster, "DataScienceCluster")
	cleanupListResources(t, tc, gvk.DSCInitialization, "DSCInitialization")

	// Cleanup AuthConfig
	cleanupResource(t, tc, gvk.Auth, types.NamespacedName{
		Name:      serviceApi.AuthInstanceName,
		Namespace: tc.AppsNamespace,
	}, "AuthConfig")
}
