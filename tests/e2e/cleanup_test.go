package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/kueue"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

// CleanupPreviousTestResources removes leftover resources from previous test runs.
// This is called at the start of test suites to ensure a clean environment.
// It handles: DSC, DSCI, AuthConfig, Kueue resources, and ResourceQuotas.
func CleanupPreviousTestResources(t *testing.T) {
	t.Helper()

	// Initialize the test context.
	tc, err := NewTestContext(t)
	require.NoError(t, err, "Failed to initialize test context")

	// Cleanup existing DataScienceCluster and DSCInitialization (single instances)
	cleanupCoreOperatorResources(t, tc)

	// Delete the entire applications namespace - this removes all component resources at once
	// (AuthConfig, ResourceQuotas, Kueue resources, etc.) and the operator will recreate as needed
	t.Logf("Cleaning up applications namespace: %s", tc.AppsNamespace)
	tc.DeleteResource(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: tc.AppsNamespace}),
		WithIgnoreNotFound(true),
		WithWaitForDeletion(true), // Wait for namespace deletion to complete before proceeding
	)

	// Also clean up the RHOAI applications namespace if it exists (for RHOAI mode)
	rhoaiAppsNamespace := "redhat-ods-applications"
	if tc.AppsNamespace != rhoaiAppsNamespace {
		t.Logf("Cleaning up RHOAI applications namespace: %s", rhoaiAppsNamespace)
		tc.DeleteResource(
			WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: rhoaiAppsNamespace}),
			WithIgnoreNotFound(true),
			WithWaitForDeletion(true),
		)
	}

	// Recreate the applications namespace immediately after deletion to avoid cache issues
	// The operator's cache expects this namespace to exist at startup
	t.Logf("Recreating applications namespace: %s", tc.AppsNamespace)
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(CreateNamespaceWithLabels(tc.AppsNamespace, nil)),
	)

	// Also recreate the RHOAI applications namespace to avoid operator cache errors
	// The RHOAI-mode operator has this namespace hardcoded in its cache configuration
	if tc.AppsNamespace != rhoaiAppsNamespace {
		t.Logf("Recreating RHOAI applications namespace for cache: %s", rhoaiAppsNamespace)
		tc.EventuallyResourceCreatedOrUpdated(
			WithObjectToCreate(CreateNamespaceWithLabels(rhoaiAppsNamespace, nil)),
		)
	}

	// Cleanup Kueue cluster-scoped resources
	cleanupKueueOperatorAndResources(t, tc)

	// Cleanup CodeFlare resources
	cleanupCodeFlareTestResources(t, tc)
}

// cleanupCoreOperatorResources deletes DataScienceCluster and DSCInitialization resources.
func cleanupCoreOperatorResources(t *testing.T, tc *TestContext) {
	t.Helper()

	deleteResources := func(gvk schema.GroupVersionKind) {
		t.Logf("Cleaning up %s with bulk operation", gvk.Kind)

		tc.DeleteResources(
			WithMinimalObject(gvk, types.NamespacedName{}),
			WithWaitForDeletion(true),
			WithIgnoreNotFound(true),
		)
	}

	deleteResources(gvk.DataScienceCluster)
	deleteResources(gvk.DSCInitialization)

	// Delete Auth CR (cluster-scoped, not affected by namespace deletion)
	deleteResources(gvk.Auth)
}

func cleanupKueueOperatorAndResources(t *testing.T, tc *TestContext) {
	t.Helper()

	cleanupKueueTestResources(t, tc)

	// Uninstall ocp kueue operator if present
	t.Logf("Uninstalling kueue operator")
	tc.UninstallOperator(types.NamespacedName{Name: kueueOpName, Namespace: kueueOcpOperatorNamespace})
}

// cleanupKueueTestResources cleans up Kueue test resources including ClusterQueue, LocalQueue, and test namespace.
func cleanupKueueTestResources(t *testing.T, tc *TestContext) {
	t.Helper()

	// Cleanup additional Kueue resources
	t.Logf("Cleaning up Kueue resources")
	clusterScopedResources := []struct {
		gvk            schema.GroupVersionKind
		namespacedName types.NamespacedName
	}{
		{gvk.Namespace, types.NamespacedName{Name: kueueTestManagedNamespace}},
		{gvk.Namespace, types.NamespacedName{Name: kueueTestLegacyManagedNamespace}},
		{gvk.Namespace, types.NamespacedName{Name: kueueTestWebhookNonManagedNamespace}},
		{gvk.Namespace, types.NamespacedName{Name: kueueTestHardwareProfileNamespace}},
		{gvk.ClusterQueue, types.NamespacedName{Name: kueueDefaultClusterQueueName}},
		{gvk.KueueConfigV1, types.NamespacedName{Name: kueue.KueueCRName}},
	}

	t.Logf("Will attempt to delete %d Kueue resources", len(clusterScopedResources))

	for _, resource := range clusterScopedResources {
		t.Logf("Attempting to delete %s %s/%s", resource.gvk.Kind, resource.namespacedName.Namespace, resource.namespacedName.Name)

		// For CRD-dependent resources, skip finalizer removal to avoid fetching non-existent resources
		removeFinalizersOnDelete := true
		if resource.gvk.Kind == gvk.KueueConfigV1.Kind || resource.gvk.Kind == gvk.ClusterQueue.Kind {
			removeFinalizersOnDelete = false
		}

		tc.DeleteResource(
			WithMinimalObject(resource.gvk, resource.namespacedName),
			WithIgnoreNotFound(true),
			WithRemoveFinalizersOnDelete(removeFinalizersOnDelete),
			WithWaitForDeletion(false),
			WithAcceptableErr(meta.IsNoMatchError, "IsNoMatchError"),
		)
		t.Logf("Successfully processed deletion of %s %s/%s", resource.gvk.Kind, resource.namespacedName.Namespace, resource.namespacedName.Name)
	}
}

func cleanupCodeFlareTestResources(t *testing.T, tc *TestContext) {
	t.Helper()

	// Cleanup CodeFlare resources
	t.Logf("Cleaning up CodeFlare resources")
	tc.DeleteResource(
		WithMinimalObject(gvk.CodeFlare, types.NamespacedName{Name: defaultCodeFlareComponentName}),
		WithIgnoreNotFound(true),
		WithWaitForDeletion(false),
		WithAcceptableErr(meta.IsNoMatchError, "IsNoMatchError"),
	)
}
