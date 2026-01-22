package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/kueue"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

// workloadGVK is used to cleanup Kueue Workloads that may block namespace deletion.
var workloadGVK = schema.GroupVersionKind{
	Group:   "kueue.x-k8s.io",
	Version: "v1beta1",
	Kind:    "Workload",
}

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
		WithWaitForDeletion(true),
	)

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

	cleanupAllKueueTestResources(t, tc)

	// Uninstall ocp kueue operator if present
	t.Logf("Uninstalling kueue operator")
	tc.UninstallOperator(types.NamespacedName{Name: kueueOpName, Namespace: kueueOcpOperatorNamespace})
}

// cleanupAllKueueTestResources cleans up Kueue test namespaces (by label) and cluster-scoped resources.
// It discovers labeled test namespaces, removes Workload finalizers, and deletes the namespaces.
func cleanupAllKueueTestResources(t *testing.T, tc *TestContext) {
	t.Helper()

	t.Logf("Finding Kueue test namespaces by label %s=%s", kueueTestNamespaceLabel, kueueTestNamespaceLabelValue)
	nsList := &corev1.NamespaceList{}
	// Errors are logged but not fatal - cleanup runs at suite start and should not block other tests.
	// Leftover namespaces from previous runs may persist but won't affect tests using unique names.
	if err := tc.Client().List(tc.Context(), nsList, client.MatchingLabels{
		kueueTestNamespaceLabel: kueueTestNamespaceLabelValue,
	}); err != nil {
		t.Logf("Warning: failed to list Kueue test namespaces, leftovers may persist: %v", err)
		nsList.Items = nil
	}
	t.Logf("Found %d Kueue test namespaces", len(nsList.Items))

	// Delete resources with finalizers that may block namespace deletion.
	for _, ns := range nsList.Items {
		nsName := ns.GetName()
		t.Logf("Cleaning up Workloads and Notebooks in test namespace %s", nsName)
		for _, resourceGVK := range []schema.GroupVersionKind{workloadGVK, gvk.Notebook} {
			tc.DeleteResources(
				WithMinimalObject(resourceGVK, types.NamespacedName{}),
				WithNamespaceFilter(nsName),
				WithIgnoreNotFound(true),
				WithRemoveFinalizersOnDelete(true),
				WithWaitForDeletion(false),
				WithAcceptableErr(meta.IsNoMatchError, "IsNoMatchError"),
			)
		}
	}

	for _, ns := range nsList.Items {
		nsName := ns.GetName()
		t.Logf("Deleting test namespace %s", nsName)
		tc.DeleteResource(
			WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: nsName}),
			WithIgnoreNotFound(true),
			WithRemoveFinalizersOnDelete(true),
			WithWaitForDeletion(false),
		)
	}

	// Also clean up cluster-scoped resources
	cleanupKueueClusterScopedResources(t, tc)
}

// cleanupKueueClusterScopedResources cleans up cluster-scoped Kueue resources (ClusterQueue, KueueConfig).
func cleanupKueueClusterScopedResources(t *testing.T, tc *TestContext) {
	t.Helper()

	t.Logf("Cleaning up Kueue cluster-scoped resources")
	clusterScopedResources := []struct {
		gvk            schema.GroupVersionKind
		namespacedName types.NamespacedName
	}{
		{gvk.ClusterQueue, types.NamespacedName{Name: kueueDefaultClusterQueueName}},
		{gvk.KueueConfigV1, types.NamespacedName{Name: kueue.KueueCRName}},
	}

	t.Logf("Will attempt to delete %d Kueue resources", len(clusterScopedResources))

	for _, resource := range clusterScopedResources {
		t.Logf("Attempting to delete %s %s/%s", resource.gvk.Kind, resource.namespacedName.Namespace, resource.namespacedName.Name)

		tc.DeleteResource(
			WithMinimalObject(resource.gvk, resource.namespacedName),
			WithIgnoreNotFound(true),
			WithRemoveFinalizersOnDelete(true),
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
