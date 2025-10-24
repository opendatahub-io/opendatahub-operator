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
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
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
	t.Parallel()
	testCases := []struct {
		name                    string
		expectResourcesNotEmpty bool
		setupRR                 func(t *testing.T) *odhtypes.ReconciliationRequest
		validate                func(t *testing.T, rr *odhtypes.ReconciliationRequest)
	}{
		{
			name:                    "WithOdhDashboardConfig",
			expectResourcesNotEmpty: true,
			setupRR: func(t *testing.T) *odhtypes.ReconciliationRequest {
				t.Helper()
				g := NewWithT(t)
				cli, err := fakeclient.New()
				g.Expect(err).ShouldNot(HaveOccurred())

				// Create a resource with OdhDashboardConfig GVK
				odhDashboardConfig := &unstructured.Unstructured{}
				odhDashboardConfig.SetGroupVersionKind(gvk.OdhDashboardConfig)
				odhDashboardConfig.SetName(testConfigName)
				odhDashboardConfig.SetNamespace(TestNamespace)

				rr := SetupTestReconciliationRequestSimple(t)
				rr.Client = cli
				rr.Resources = []unstructured.Unstructured{*odhDashboardConfig}
				return rr
			},
			validate: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				g := NewWithT(t)
				// Check that the annotation was set
				g.Expect(rr.Resources[0].GetAnnotations()).Should(HaveKey(annotations.ManagedByODHOperator))
				g.Expect(rr.Resources[0].GetAnnotations()[annotations.ManagedByODHOperator]).Should(Equal("false"))
			},
		},
		{
			name:                    "WithoutOdhDashboardConfig",
			expectResourcesNotEmpty: true,
			setupRR: func(t *testing.T) *odhtypes.ReconciliationRequest {
				t.Helper()
				g := NewWithT(t)
				cli, err := fakeclient.New()
				g.Expect(err).ShouldNot(HaveOccurred())

				// Create a resource without OdhDashboardConfig GVK
				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testConfigName,
						Namespace: TestNamespace,
					},
				}
				configMap.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "",
					Version: "v1",
					Kind:    "ConfigMap",
				})

				rr := SetupTestReconciliationRequestSimple(t)
				rr.Client = cli
				rr.Resources = []unstructured.Unstructured{*unstructuredFromObject(t, configMap)}
				return rr
			},
			validate: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				g := NewWithT(t)
				// Check that no annotation was set
				g.Expect(rr.Resources[0].GetAnnotations()).ShouldNot(HaveKey(annotations.ManagedByODHOperator))
			},
		},
		{
			name:                    "EmptyResources",
			expectResourcesNotEmpty: false,
			setupRR: func(t *testing.T) *odhtypes.ReconciliationRequest {
				t.Helper()
				g := NewWithT(t)
				cli, err := fakeclient.New()
				g.Expect(err).ShouldNot(HaveOccurred())

				rr := SetupTestReconciliationRequestSimple(t)
				rr.Client = cli
				rr.Resources = []unstructured.Unstructured{}
				return rr
			},
			validate: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				g := NewWithT(t)

				// Assert that resources slice is empty
				g.Expect(rr.Resources).Should(BeEmpty(), "Resources should be empty")

				// Verify no annotations were added to any resources (since there are none)
				// This ensures the function handles empty resources gracefully
				for _, resource := range rr.Resources {
					g.Expect(resource.GetAnnotations()).ShouldNot(HaveKey(annotations.ManagedByODHOperator))
				}
			},
		},
		{
			name:                    "MultipleResources",
			expectResourcesNotEmpty: true,
			setupRR: func(t *testing.T) *odhtypes.ReconciliationRequest {
				t.Helper()
				g := NewWithT(t)
				cli, err := fakeclient.New()
				g.Expect(err).ShouldNot(HaveOccurred())

				// Create multiple resources, one with OdhDashboardConfig GVK
				odhDashboardConfig := &unstructured.Unstructured{}
				odhDashboardConfig.SetGroupVersionKind(gvk.OdhDashboardConfig)
				odhDashboardConfig.SetName(testConfigName)
				odhDashboardConfig.SetNamespace(TestNamespace)

				configMap := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-configmap",
						Namespace: TestNamespace,
					},
				}
				configMap.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "",
					Version: "v1",
					Kind:    "ConfigMap",
				})

				rr := SetupTestReconciliationRequestSimple(t)
				rr.Client = cli
				rr.Resources = []unstructured.Unstructured{
					*unstructuredFromObject(t, configMap),
					*odhDashboardConfig,
				}
				return rr
			},
			validate: func(t *testing.T, rr *odhtypes.ReconciliationRequest) {
				t.Helper()
				g := NewWithT(t)
				g.Expect(rr.Resources).Should(HaveLen(2))

				for _, resource := range rr.Resources {
					if resource.GetObjectKind().GroupVersionKind() == gvk.OdhDashboardConfig && resource.GetName() == testConfigName {
						g.Expect(resource.GetAnnotations()).Should(HaveKey(annotations.ManagedByODHOperator))
						g.Expect(resource.GetAnnotations()[annotations.ManagedByODHOperator]).Should(Equal("false"))
					} else {
						g.Expect(resource.GetAnnotations()).ShouldNot(HaveKey(annotations.ManagedByODHOperator))
					}
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			rr := tc.setupRR(t)
			g.Expect(rr.Client).ShouldNot(BeNil(), "Client should not be nil")
			if tc.expectResourcesNotEmpty {
				g.Expect(rr.Resources).ShouldNot(BeEmpty(), "Resources should not be empty")
			}

			ctx := t.Context()
			err := dashboardctrl.CustomizeResources(ctx, rr)
			g.Expect(err).ShouldNot(HaveOccurred())

			tc.validate(t, rr)
		})
	}
}

// Helper function to convert any object to unstructured.
func unstructuredFromObject(t *testing.T, obj client.Object) *unstructured.Unstructured {
	t.Helper()

	unstructuredObj, err := resources.ObjectToUnstructured(testScheme, obj)
	if err != nil {
		t.Fatalf("ObjectToUnstructured failed for object %+v: %v", obj, err)
	}
	return unstructuredObj
}
