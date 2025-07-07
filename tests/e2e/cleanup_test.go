package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"
)

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

	// Cleanup Kueue Test Resources
	cleanupKueueTestResources(t, tc)
}

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
func uninstallOperator(t *testing.T, tc *TestContext, operatorNamespacedName types.NamespacedName) { //nolint:thelper,unused
	uninstallOperatorWithChannel(t, tc, operatorNamespacedName, defaultOperatorChannel)
}

// uninstallOperatorWithChannel delete an operator install subscription to a specific channel if exists.
func uninstallOperatorWithChannel(t *testing.T, tc *TestContext, operatorNamespacedName types.NamespacedName, channel string) { //nolint:thelper,unparam
	if found, err := tc.CheckOperatorExists(operatorNamespacedName.Name); found && err == nil {
		t.Logf("Uninstalling %s operator", operatorNamespacedName)
		ro := tc.NewResourceOptions(WithMinimalObject(gvk.Subscription, operatorNamespacedName))
		operatorSubscription, _ := tc.ensureResourceExistsOrNil(ro)

		if operatorSubscription != nil {
			csv, foundCsv, errCsv := unstructured.NestedString(operatorSubscription.UnstructuredContent(), "status", "currentCSV")
			installPlan, foundPlan, errPlan := unstructured.NestedString(operatorSubscription.UnstructuredContent(), "status", "installPlanRef", "name")

			t.Logf("Found subscription %v deleting it.", operatorNamespacedName)
			tc.DeleteResource(WithMinimalObject(gvk.Subscription, operatorNamespacedName), WithWaitForDeletion(true))

			if foundCsv && errCsv == nil {
				t.Logf("Found CSV %s in operator subscription %v deleting it.", csv, operatorNamespacedName)
				tc.DeleteResource(WithMinimalObject(gvk.ClusterServiceVersion, types.NamespacedName{Name: csv, Namespace: operatorSubscription.GetNamespace()}), WithWaitForDeletion(true))
			}
			if foundPlan && errPlan == nil {
				t.Logf("Found install plan %s in operator subscription %v deleting it.", installPlan, operatorNamespacedName)
				tc.DeleteResource(WithMinimalObject(gvk.InstallPlan, types.NamespacedName{Name: installPlan, Namespace: operatorSubscription.GetNamespace()}), WithWaitForDeletion(true))
			}
		}
	}
}

// cleanupKueueTestResources cleans up Kueue test resources including ClusterQueue, LocalQueue, and test namespace.
func cleanupKueueTestResources(t *testing.T, tc *TestContext) {
	t.Helper()

	// Delete kueue cluster queue if present
	_ = cleanupResourceIgnoringMissing(t, tc, types.NamespacedName{Name: kueueDefaultClusterQueueName}, gvk.ClusterQueue, true)
	// Delete kueue local queue if present
	_ = cleanupResourceIgnoringMissing(t, tc, types.NamespacedName{Name: kueueDefaultLocalQueueName, Namespace: kueueTestManagedNamespace}, gvk.LocalQueue, true)
	// Delete kueue cluster config if present
	_ = cleanupResourceIgnoringMissing(t, tc, types.NamespacedName{Name: kueueDefaultOperatorConfigName}, gvk.KueueConfigV1, false)
	// Delete test managed namespace if present
	_ = cleanupResourceIgnoringMissing(t, tc, types.NamespacedName{Name: kueueTestManagedNamespace}, gvk.Namespace, false)

	// Uninstall ocp kueue operator if present
	uninstallOperatorWithChannel(t, tc, types.NamespacedName{Name: kueueOpName, Namespace: kueueOcpOperatorNamespace}, kueueOcpOperatorChannel)
}

func cleanupResourceIgnoringMissing(t *testing.T, tc *TestContext, namespacedName types.NamespacedName, crdGvk schema.GroupVersionKind, removeFinalizers bool) error { //nolint:thelper,lll
	t.Logf("Deleting (if present) resource %s of type: %v in namespace: %s (removing finalizers: %t)", namespacedName.Name, crdGvk, namespacedName.Namespace, removeFinalizers)
	// Return if crdGvk does not exist in the cluster
	hasCrd, err := cluster.HasCRD(tc.Context(), tc.Client(), crdGvk)
	if err != nil {
		return err
	}
	if !hasCrd {
		return nil
	}

	// If the namespacedName.Namespace is passed, return if it does not exist in the cluster
	if len(namespacedName.Namespace) > 0 && namespacedName.Namespace != metav1.NamespaceAll {
		ro := tc.NewResourceOptions(WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: namespacedName.Namespace}))
		namespaceExists, err := tc.ensureResourceExistsOrNil(ro)
		if err != nil {
			return err
		}
		if namespaceExists == nil {
			return nil
		}
	}

	// If the resource does not exist, return
	ro := tc.NewResourceOptions(WithMinimalObject(crdGvk, namespacedName))
	resorceExists, err := tc.ensureResourceExistsOrNil(ro)
	if err != nil {
		return err
	}
	if resorceExists == nil {
		return nil
	}

	// Delete the resource
	if removeFinalizers {
		tc.EventuallyResourceCreatedOrUpdated(
			WithMinimalObject(crdGvk, namespacedName),
			WithMutateFunc(testf.Transform(`.metadata.finalizers = []`)),
			WithIgnoreNotFound(true),
		)
	}
	tc.DeleteResource(
		WithMinimalObject(crdGvk, namespacedName),
		WithIgnoreNotFound(true),
	)
	return nil
}
