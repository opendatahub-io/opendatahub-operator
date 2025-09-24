package dashboard_test

import (
	"fmt"
	"testing"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dashboardctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
)

const (
	testConfigName    = "test-config"
	managedAnnotation = "opendatahub.io/managed"
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
	t.Run("WithOdhDashboardConfig", testCustomizeResourcesWithOdhDashboardConfig)
	t.Run("WithoutOdhDashboardConfig", testCustomizeResourcesWithoutOdhDashboardConfig)
	t.Run("EmptyResources", testCustomizeResourcesEmptyResources)
	t.Run("MultipleResources", testCustomizeResourcesMultipleResources)
}

func testCustomizeResourcesWithOdhDashboardConfig(t *testing.T) {
	t.Helper()
	cli, err := fakeclient.New()
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	// Create a resource with OdhDashboardConfig GVK
	odhDashboardConfig := &unstructured.Unstructured{}
	odhDashboardConfig.SetGroupVersionKind(gvk.OdhDashboardConfig)
	odhDashboardConfig.SetName(testConfigName)
	odhDashboardConfig.SetNamespace(dashboardctrl.TestNamespace)

	rr := dashboardctrl.SetupTestReconciliationRequestSimple(t)
	rr.Client = cli
	rr.Resources = []unstructured.Unstructured{*odhDashboardConfig}

	ctx := t.Context()
	err = dashboardctrl.CustomizeResources(ctx, rr)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	// Check that the annotation was set
	gomega.NewWithT(t).Expect(rr.Resources[0].GetAnnotations()).Should(gomega.HaveKey(managedAnnotation))
	gomega.NewWithT(t).Expect(rr.Resources[0].GetAnnotations()[managedAnnotation]).Should(gomega.Equal("false"))
}

func testCustomizeResourcesWithoutOdhDashboardConfig(t *testing.T) {
	t.Helper()
	cli, err := fakeclient.New()
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	// Create a resource without OdhDashboardConfig GVK
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testConfigName,
			Namespace: dashboardctrl.TestNamespace,
		},
	}
	configMap.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ConfigMap",
	})

	rr := dashboardctrl.SetupTestReconciliationRequestSimple(t)
	rr.Client = cli
	rr.Resources = []unstructured.Unstructured{*unstructuredFromObject(t, configMap)}

	ctx := t.Context()
	err = dashboardctrl.CustomizeResources(ctx, rr)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	// Check that no annotation was set
	gomega.NewWithT(t).Expect(rr.Resources[0].GetAnnotations()).ShouldNot(gomega.HaveKey(managedAnnotation))
}

func testCustomizeResourcesEmptyResources(t *testing.T) {
	t.Helper()
	cli, err := fakeclient.New()
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	rr := dashboardctrl.SetupTestReconciliationRequestSimple(t)
	rr.Client = cli
	rr.Resources = []unstructured.Unstructured{}

	ctx := t.Context()
	err = dashboardctrl.CustomizeResources(ctx, rr)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())
}

func testCustomizeResourcesMultipleResources(t *testing.T) {
	t.Helper()
	cli, err := fakeclient.New()
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	// Create multiple resources, one with OdhDashboardConfig GVK
	odhDashboardConfig := &unstructured.Unstructured{}
	odhDashboardConfig.SetGroupVersionKind(gvk.OdhDashboardConfig)
	odhDashboardConfig.SetName(testConfigName)
	odhDashboardConfig.SetNamespace(dashboardctrl.TestNamespace)

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap",
			Namespace: dashboardctrl.TestNamespace,
		},
	}
	configMap.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "ConfigMap",
	})

	rr := dashboardctrl.SetupTestReconciliationRequestSimple(t)
	rr.Client = cli
	rr.Resources = []unstructured.Unstructured{
		*unstructuredFromObject(t, configMap),
		*odhDashboardConfig,
	}

	ctx := t.Context()
	err = dashboardctrl.CustomizeResources(ctx, rr)
	gomega.NewWithT(t).Expect(err).ShouldNot(gomega.HaveOccurred())

	// Find resources by GVK and name instead of relying on array order
	var odhDashboardConfigResource *unstructured.Unstructured
	var configMapResource *unstructured.Unstructured

	for i := range rr.Resources {
		resource := &rr.Resources[i]
		resourceGVK := resource.GetObjectKind().GroupVersionKind()

		// Find OdhDashboardConfig resource
		if resourceGVK == gvk.OdhDashboardConfig && resource.GetName() == testConfigName {
			odhDashboardConfigResource = resource
		}

		// Find ConfigMap resource
		if resourceGVK.Kind == "ConfigMap" && resource.GetName() == "test-configmap" {
			configMapResource = resource
		}
	}

	// Verify we found both resources
	gomega.NewWithT(t).Expect(odhDashboardConfigResource).ShouldNot(gomega.BeNil(), "OdhDashboardConfig resource should be found")
	gomega.NewWithT(t).Expect(configMapResource).ShouldNot(gomega.BeNil(), "ConfigMap resource should be found")

	// Check that only the OdhDashboardConfig resource got the annotation
	gomega.NewWithT(t).Expect(configMapResource.GetAnnotations()).ShouldNot(gomega.HaveKey(managedAnnotation))
	gomega.NewWithT(t).Expect(odhDashboardConfigResource.GetAnnotations()).Should(gomega.HaveKey(managedAnnotation))
	gomega.NewWithT(t).Expect(odhDashboardConfigResource.GetAnnotations()[managedAnnotation]).Should(gomega.Equal("false"))
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
		// Log the error for debugging but create a fallback unstructured object
		t.Logf("ObjectToUnstructured failed for object %+v with GVK %+v, creating fallback unstructured: %v", obj, originalGVK, err)

		// Create a basic Unstructured with the original GVK as fallback
		fallback := &unstructured.Unstructured{}
		fallback.SetGroupVersionKind(originalGVK)
		fallback.SetName(obj.GetName())
		fallback.SetNamespace(obj.GetNamespace())
		fallback.SetLabels(obj.GetLabels())
		fallback.SetAnnotations(obj.GetAnnotations())
		return fallback
	}
	return unstructuredObj
}
