package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

// cleanupResource logs and deletes the specified resource if it exists.
func cleanupResource(t *testing.T, tc *TestContext, kind schema.GroupVersionKind, name types.NamespacedName, label string) { //nolint:thelper
	t.Logf("Cleaning up %s if present", label)
	tc.DeleteResource(
		WithMinimalObject(kind, name),
		WithWaitForDeletion(true),
	)
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

// uninstallOperator delete an operator install subscription for the stable channel if exists.
func uninstallOperator(t *testing.T, tc *TestContext, operatorName string, operatorNamespace string) { //nolint:thelper
	uninstallOperatorWithChannel(t, tc, operatorName, operatorNamespace, defaultOperatorChannel)
}

// uninstallOperatorWithChannel delete an operator install subscription to a specific channel if exists.
func uninstallOperatorWithChannel(t *testing.T, tc *TestContext, operatorName string, operatorNamespace string, channel string) { //nolint:thelper,unparam
	if found, err := tc.CheckOperatorExists(operatorName); found && err == nil {
		t.Logf("Deleting %s", operatorName)
		namespacedName := types.NamespacedName{Name: operatorName, Namespace: operatorNamespace}
		ro := tc.NewResourceOptions(WithMinimalObject(gvk.Subscription, namespacedName))
		kueueOcpOperatorSubscription, _ := tc.ensureResourceExistsOrNil(ro)

		if kueueOcpOperatorSubscription != nil {
			csv, found, err := unstructured.NestedString(kueueOcpOperatorSubscription.UnstructuredContent(), "status", "currentCSV")
			if !found || err != nil {
				t.Logf("subscription %v for kueue operator found, .status.currentCSV expected to be present: %v with no error, Error: %v", kueueOcpOperatorSubscription, csv, err)
				tc.DeleteResource(WithMinimalObject(gvk.Subscription, namespacedName))
			} else {
				tc.DeleteResource(WithMinimalObject(gvk.Subscription, namespacedName))
				tc.DeleteResource(WithMinimalObject(gvk.ClusterServiceVersion, types.NamespacedName{Name: csv, Namespace: kueueOcpOperatorSubscription.GetNamespace()}))
			}

			tc.DeleteResource(
				WithMinimalObject(gvk.Subscription, types.NamespacedName{Name: kueueOcpOperatorSubscription.GetName(), Namespace: kueueOcpOperatorSubscription.GetNamespace()}))
		}
		t.Logf("Deleted %s", operatorName)
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

	// Uninstall ocp kueue operator if present
	uninstallOperatorWithChannel(t, tc, kueueOpName, kueueOcpOperatorNamespace, kueueOcpOperatorChannel)
}
