package dashboard_test

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dashboardctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard/dashboard_test"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

const (
	testConfigName = "test-config"
)

// testScheme is a shared scheme for testing, initialized once.
var testScheme = createTestScheme()

// createTestScheme creates a new scheme and adds corev1 types to it.
// It panics if AddToScheme fails to ensure test setup failures are visible.
func createTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		panic(fmt.Sprintf("Failed to add corev1 to scheme: %v", err))
	}
	return s
}

func TestCustomizeResources(t *testing.T) {
	t.Run("WithOdhDashboardConfig", func(t *testing.T) {
		cli, err := fakeclient.New()
		NewWithT(t).Expect(err).ShouldNot(HaveOccurred())

		// Create a resource with OdhDashboardConfig GVK
		odhDashboardConfig := &unstructured.Unstructured{}
		odhDashboardConfig.SetGroupVersionKind(gvk.OdhDashboardConfig)
		odhDashboardConfig.SetName(testConfigName)
		odhDashboardConfig.SetNamespace(dashboard_test.TestNamespace)

		resources := []unstructured.Unstructured{*odhDashboardConfig}
		testCustomizeResourcesWithOdhDashboardConfig(t, cli, resources)
	})
	t.Run("WithoutOdhDashboardConfig", testCustomizeResourcesWithoutOdhDashboardConfig)
	t.Run("EmptyResources", testCustomizeResourcesEmptyResources)
	t.Run("MultipleResources", testCustomizeResourcesMultipleResources)
}

func testCustomizeResourcesWithOdhDashboardConfig(t *testing.T, cli client.Client, resources []unstructured.Unstructured) {
	t.Helper()
	NewWithT(t).Expect(cli).ShouldNot(BeNil(), "Client should not be nil")
	NewWithT(t).Expect(resources).ShouldNot(BeEmpty(), "Resources should not be empty")

	rr := dashboard_test.SetupTestReconciliationRequestSimple(t)
	rr.Client = cli
	rr.Resources = resources

	ctx := t.Context()
	err := dashboardctrl.CustomizeResources(ctx, rr)
	NewWithT(t).Expect(err).ShouldNot(HaveOccurred())

	// Check that the annotation was set
	NewWithT(t).Expect(rr.Resources[0].GetAnnotations()).Should(HaveKey(annotations.ManagedByODHOperator))
	NewWithT(t).Expect(rr.Resources[0].GetAnnotations()[annotations.ManagedByODHOperator]).Should(Equal("false"))
}

func testCustomizeResourcesWithoutOdhDashboardConfig(t *testing.T) {
	t.Helper()
	cli, err := fakeclient.New()
	NewWithT(t).Expect(err).ShouldNot(HaveOccurred())

	// Create a resource without OdhDashboardConfig GVK
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testConfigName,
			Namespace: dashboard_test.TestNamespace,
		},
	}
	configMap.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ConfigMap",
	})

	rr := dashboard_test.SetupTestReconciliationRequestSimple(t)
	rr.Client = cli
	rr.Resources = []unstructured.Unstructured{*unstructuredFromObject(t, configMap)}

	ctx := t.Context()
	err = dashboardctrl.CustomizeResources(ctx, rr)
	NewWithT(t).Expect(err).ShouldNot(HaveOccurred())

	// Check that no annotation was set
	NewWithT(t).Expect(rr.Resources[0].GetAnnotations()).ShouldNot(HaveKey(annotations.ManagedByODHOperator))
}

func testCustomizeResourcesEmptyResources(t *testing.T) {
	t.Helper()
	cli, err := fakeclient.New()
	NewWithT(t).Expect(err).ShouldNot(HaveOccurred())

	rr := dashboard_test.SetupTestReconciliationRequestSimple(t)
	rr.Client = cli
	rr.Resources = []unstructured.Unstructured{}

	ctx := t.Context()
	err = dashboardctrl.CustomizeResources(ctx, rr)
	NewWithT(t).Expect(err).ShouldNot(HaveOccurred())
}

func testCustomizeResourcesMultipleResources(t *testing.T) {
	t.Helper()
	cli, err := fakeclient.New()
	NewWithT(t).Expect(err).ShouldNot(HaveOccurred())

	// Create multiple resources, one with OdhDashboardConfig GVK
	odhDashboardConfig := &unstructured.Unstructured{}
	odhDashboardConfig.SetGroupVersionKind(gvk.OdhDashboardConfig)
	odhDashboardConfig.SetName(testConfigName)
	odhDashboardConfig.SetNamespace(dashboard_test.TestNamespace)

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap",
			Namespace: dashboard_test.TestNamespace,
		},
	}
	configMap.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ConfigMap",
	})

	rr := dashboard_test.SetupTestReconciliationRequestSimple(t)
	rr.Client = cli
	rr.Resources = []unstructured.Unstructured{
		*unstructuredFromObject(t, configMap),
		*odhDashboardConfig,
	}

	ctx := t.Context()
	err = dashboardctrl.CustomizeResources(ctx, rr)
	NewWithT(t).Expect(err).ShouldNot(HaveOccurred())

	NewWithT(t).Expect(rr.Resources).Should(HaveLen(2))

	for _, resource := range rr.Resources {
		if resource.GetObjectKind().GroupVersionKind() == gvk.OdhDashboardConfig && resource.GetName() == testConfigName {
			NewWithT(t).Expect(resource.GetAnnotations()).Should(HaveKey(annotations.ManagedByODHOperator))
			NewWithT(t).Expect(resource.GetAnnotations()[annotations.ManagedByODHOperator]).Should(Equal("false"))
		} else {
			NewWithT(t).Expect(resource.GetAnnotations()).ShouldNot(HaveKey(annotations.ManagedByODHOperator))
		}
	}
}

// Helper function to convert any object to unstructured.
func unstructuredFromObject(t *testing.T, obj client.Object) *unstructured.Unstructured {
	t.Helper()

	// Extract the original object's GVK
	originalGVK := obj.GetObjectKind().GroupVersionKind()

	// Validate GVK - fail test if completely empty to surface setup bugs
	if originalGVK.Group == "" && originalGVK.Version == "" && originalGVK.Kind == "" {
		t.Fatalf("Object has completely empty GVK (Group/Version/Kind all empty) - this indicates a test setup issue with the object: %+v", obj)
	}

	unstructuredObj, err := resources.ObjectToUnstructured(testScheme, obj)
	if err != nil {
		t.Fatalf("ObjectToUnstructured failed for object %+v with GVK %+v: %v", obj, originalGVK, err)
	}
	return unstructuredObj
}
