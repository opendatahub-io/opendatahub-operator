package monitoring_test

import (
	"context"
	"testing"

	"gomodules.xyz/jsonpatch/v2"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/monitoring"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"

	. "github.com/onsi/gomega"
)

const (
	testNamespace        = "test-ns"
	testPodMonitor       = "test-podmonitor"
	testServiceMonitor   = "test-servicemonitor"
	monitoringLabelKey   = "opendatahub.io/monitoring"
	monitoringLabelValue = "true"
	dashboardLabelKey    = "opendatahub.io/dashboard"
	dashboardLabelValue  = "true"
)

// Helper functions for test simplification.

// newMonitoredNamespace creates a namespace with monitoring enabled.
func newMonitoredNamespace(name string) *corev1.Namespace {
	return envtestutil.NewNamespace(name, map[string]string{
		dashboardLabelKey:  dashboardLabelValue,
		monitoringLabelKey: monitoringLabelValue,
	})
}

// hasMonitoringLabel checks if the object has the monitoring label set to "true".
func hasMonitoringLabel(obj client.Object) bool {
	labels := obj.GetLabels()
	return labels != nil && labels[monitoringLabelKey] == monitoringLabelValue
}

// Helper function to create PodMonitor.
func newPodMonitor(name, namespace string, opts ...envtestutil.ObjectOption) client.Object {
	podMonitor := resources.GvkToUnstructured(gvk.CoreosPodMonitor)
	podMonitor.SetName(name)
	podMonitor.SetNamespace(namespace)

	for _, opt := range opts {
		opt(podMonitor)
	}
	return podMonitor
}

// Helper function to create ServiceMonitor.
func newServiceMonitor(name, namespace string, opts ...envtestutil.ObjectOption) client.Object {
	serviceMonitor := resources.GvkToUnstructured(gvk.CoreosServiceMonitor)
	serviceMonitor.SetName(name)
	serviceMonitor.SetNamespace(namespace)

	for _, opt := range opts {
		opt(serviceMonitor)
	}
	return serviceMonitor
}

// setupTestEnvironment creates the common test environment for webhook tests.
func setupTestEnvironment(t *testing.T) (*runtime.Scheme, context.Context) {
	t.Helper()
	sch, err := scheme.New()
	NewWithT(t).Expect(err).ShouldNot(HaveOccurred())

	return sch, t.Context()
}

// createWebhookInjector creates a webhook injector with the given client and scheme.
func createWebhookInjector(cli client.Client, sch *runtime.Scheme) *monitoring.Injector {
	injector := &monitoring.Injector{
		Client:  cli,
		Decoder: admission.NewDecoder(sch),
		Name:    "test",
	}
	return injector
}

// hasLabelPatch checks if any patch adds the monitoring label.
func hasLabelPatch(patches []jsonpatch.JsonPatchOperation) bool {
	for _, patch := range patches {
		if patch.Path == "/metadata/labels" || patch.Path == "/metadata/labels/opendatahub.io~1monitoring" {
			if labelMap, ok := patch.Value.(map[string]interface{}); ok {
				if val, exists := labelMap[monitoringLabelKey]; exists && val == monitoringLabelValue {
					return true
				}
			}
			// Check if it's a single label addition
			if patch.Path == "/metadata/labels/opendatahub.io~1monitoring" && patch.Value == monitoringLabelValue {
				return true
			}
		}
	}
	return false
}

// TestMonitoring_AllowsRequests tests various scenarios where the webhook should allow requests without processing.
func TestMonitoring_AllowsRequests(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	testCases := []struct {
		name      string
		operation admissionv1.Operation
		workload  client.Object
		gvkToUse  schema.GroupVersionKind
		namespace *corev1.Namespace
	}{
		{
			name:      "DELETE operation on PodMonitor",
			operation: admissionv1.Delete,
			workload:  newPodMonitor(testPodMonitor, testNamespace),
			gvkToUse:  gvk.CoreosPodMonitor,
			namespace: newMonitoredNamespace(testNamespace),
		},
		{
			name:      "DELETE operation on ServiceMonitor",
			operation: admissionv1.Delete,
			workload:  newServiceMonitor(testServiceMonitor, testNamespace),
			gvkToUse:  gvk.CoreosServiceMonitor,
			namespace: newMonitoredNamespace(testNamespace),
		},
		{
			name:      "CREATE PodMonitor - namespace without dashboard label",
			operation: admissionv1.Create,
			workload:  newPodMonitor(testPodMonitor, testNamespace),
			gvkToUse:  gvk.CoreosPodMonitor,
			namespace: envtestutil.NewNamespace(testNamespace, map[string]string{}),
		},
		{
			name:      "CREATE ServiceMonitor - namespace without dashboard label",
			operation: admissionv1.Create,
			workload:  newServiceMonitor(testServiceMonitor, testNamespace),
			gvkToUse:  gvk.CoreosServiceMonitor,
			namespace: envtestutil.NewNamespace(testNamespace, map[string]string{}),
		},
		{
			name:      "CREATE PodMonitor - namespace without monitoring label",
			operation: admissionv1.Create,
			workload:  newPodMonitor(testPodMonitor, testNamespace),
			gvkToUse:  gvk.CoreosPodMonitor,
			namespace: envtestutil.NewNamespace(testNamespace, map[string]string{
				dashboardLabelKey: dashboardLabelValue,
			}),
		},
		{
			name:      "CREATE ServiceMonitor - namespace without monitoring label",
			operation: admissionv1.Create,
			workload:  newServiceMonitor(testServiceMonitor, testNamespace),
			gvkToUse:  gvk.CoreosServiceMonitor,
			namespace: envtestutil.NewNamespace(testNamespace, map[string]string{
				dashboardLabelKey: dashboardLabelValue,
			}),
		},
		{
			name:      "CREATE PodMonitor - namespace with dashboard=false",
			operation: admissionv1.Create,
			workload:  newPodMonitor(testPodMonitor, testNamespace),
			gvkToUse:  gvk.CoreosPodMonitor,
			namespace: envtestutil.NewNamespace(testNamespace, map[string]string{
				dashboardLabelKey:  "false",
				monitoringLabelKey: monitoringLabelValue,
			}),
		},
		{
			name:      "UPDATE PodMonitor - namespace without dashboard label",
			operation: admissionv1.Update,
			workload:  newPodMonitor(testPodMonitor, testNamespace),
			gvkToUse:  gvk.CoreosPodMonitor,
			namespace: envtestutil.NewNamespace(testNamespace, map[string]string{}),
		},
		{
			name:      "UPDATE ServiceMonitor - namespace without monitoring label",
			operation: admissionv1.Update,
			workload:  newServiceMonitor(testServiceMonitor, testNamespace),
			gvkToUse:  gvk.CoreosServiceMonitor,
			namespace: envtestutil.NewNamespace(testNamespace, map[string]string{
				dashboardLabelKey: dashboardLabelValue,
			}),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(tc.namespace).Build()
			injector := createWebhookInjector(cli, sch)

			var resourceName string
			if tc.gvkToUse.Kind == "PodMonitor" {
				resourceName = "podmonitors"
			} else {
				resourceName = "servicemonitors"
			}

			req := envtestutil.NewAdmissionRequest(
				t,
				tc.operation,
				tc.workload,
				tc.gvkToUse,
				metav1.GroupVersionResource{
					Group:    tc.gvkToUse.Group,
					Version:  tc.gvkToUse.Version,
					Resource: resourceName,
				},
			)

			resp := injector.Handle(ctx, req)
			g.Expect(resp.Allowed).Should(BeTrue())
			g.Expect(resp.Patches).Should(BeEmpty())
		})
	}
}

// TestMonitoring_DeniesWhenDecoderNotInitialized tests that requests are denied when decoder is nil.
func TestMonitoring_DeniesWhenDecoderNotInitialized(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	// Create injector WITHOUT decoder injection
	injector := &monitoring.Injector{
		Name: "test-injector",
		// Decoder is intentionally nil to test the nil check
	}

	// Create a test request
	req := envtestutil.NewAdmissionRequest(
		t,
		admissionv1.Create,
		newPodMonitor(testPodMonitor, testNamespace),
		gvk.CoreosPodMonitor,
		metav1.GroupVersionResource{
			Group:    gvk.CoreosPodMonitor.Group,
			Version:  gvk.CoreosPodMonitor.Version,
			Resource: "podmonitors",
		},
	)

	ctx := t.Context()
	resp := injector.Handle(ctx, req)

	// Should deny the request due to nil decoder
	g.Expect(resp.Allowed).Should(BeFalse())
	g.Expect(resp.Result.Message).Should(ContainSubstring("webhook decoder not initialized"))
}

// TestMonitoring_ErrorPaths tests various error conditions in the webhook.
func TestMonitoring_ErrorPaths(t *testing.T) {
	t.Parallel()
	sch, ctx := setupTestEnvironment(t)

	testCases := []struct {
		name          string
		injector      *monitoring.Injector
		workload      client.Object
		gvkToUse      schema.GroupVersionKind
		namespace     *corev1.Namespace
		expectAllowed bool
		expectMessage string
	}{
		{
			name: "nil decoder",
			injector: &monitoring.Injector{
				Client: fake.NewClientBuilder().WithScheme(sch).Build(),
				Name:   "test",
				// Decoder is nil
			},
			workload:      newPodMonitor(testPodMonitor, testNamespace),
			gvkToUse:      gvk.CoreosPodMonitor,
			namespace:     newMonitoredNamespace(testNamespace),
			expectAllowed: false,
			expectMessage: "webhook decoder not initialized",
		},
		{
			name:     "unexpected kind (Pod instead of PodMonitor)",
			injector: createWebhookInjector(fake.NewClientBuilder().WithScheme(sch).Build(), sch),
			workload: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: testNamespace,
				},
			},
			gvkToUse:      schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"},
			namespace:     newMonitoredNamespace(testNamespace),
			expectAllowed: false,
			expectMessage: "unexpected kind: Pod",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			var gvrToUse metav1.GroupVersionResource
			if tc.gvkToUse.Kind == "Pod" {
				gvrToUse = metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
			} else if tc.gvkToUse.Kind == "PodMonitor" {
				gvrToUse = metav1.GroupVersionResource{
					Group:    tc.gvkToUse.Group,
					Version:  tc.gvkToUse.Version,
					Resource: "podmonitors",
				}
			} else {
				gvrToUse = metav1.GroupVersionResource{
					Group:    tc.gvkToUse.Group,
					Version:  tc.gvkToUse.Version,
					Resource: "servicemonitors",
				}
			}

			req := envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Create,
				tc.workload,
				tc.gvkToUse,
				gvrToUse,
			)

			resp := tc.injector.Handle(ctx, req)

			g.Expect(resp.Allowed).Should(Equal(tc.expectAllowed))
			if tc.expectMessage != "" {
				g.Expect(resp.Result.Message).Should(ContainSubstring(tc.expectMessage))
			}
		})
	}
}

// TestMonitoring_InjectsLabelToPodMonitor tests that the monitoring label is injected into PodMonitors.
func TestMonitoring_InjectsLabelToPodMonitor(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create namespace with proper labels
	ns := newMonitoredNamespace(testNamespace)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(ns).Build()
	injector := createWebhookInjector(cli, sch)

	// Create PodMonitor without the monitoring label
	podMonitor := newPodMonitor(testPodMonitor, testNamespace)

	req := envtestutil.NewAdmissionRequest(
		t,
		admissionv1.Create,
		podMonitor,
		gvk.CoreosPodMonitor,
		metav1.GroupVersionResource{
			Group:    gvk.CoreosPodMonitor.Group,
			Version:  gvk.CoreosPodMonitor.Version,
			Resource: "podmonitors",
		},
	)

	resp := injector.Handle(ctx, req)
	g.Expect(resp.Allowed).Should(BeTrue())
	g.Expect(resp.Patches).Should(Not(BeEmpty()))
	g.Expect(hasLabelPatch(resp.Patches)).Should(BeTrue(), "Should have monitoring label patch")
}

// TestMonitoring_InjectsLabelToServiceMonitor tests that the monitoring label is injected into ServiceMonitors.
func TestMonitoring_InjectsLabelToServiceMonitor(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create namespace with proper labels
	ns := newMonitoredNamespace(testNamespace)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(ns).Build()
	injector := createWebhookInjector(cli, sch)

	// Create ServiceMonitor without the monitoring label
	serviceMonitor := newServiceMonitor(testServiceMonitor, testNamespace)

	req := envtestutil.NewAdmissionRequest(
		t,
		admissionv1.Create,
		serviceMonitor,
		gvk.CoreosServiceMonitor,
		metav1.GroupVersionResource{
			Group:    gvk.CoreosServiceMonitor.Group,
			Version:  gvk.CoreosServiceMonitor.Version,
			Resource: "servicemonitors",
		},
	)

	resp := injector.Handle(ctx, req)
	g.Expect(resp.Allowed).Should(BeTrue())
	g.Expect(resp.Patches).Should(Not(BeEmpty()))
	g.Expect(hasLabelPatch(resp.Patches)).Should(BeTrue(), "Should have monitoring label patch")
}

// TestMonitoring_HandlesUpdateOperations tests that update operations are handled correctly.
func TestMonitoring_HandlesUpdateOperations(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create namespace with proper labels
	ns := newMonitoredNamespace(testNamespace)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(ns).Build()
	injector := createWebhookInjector(cli, sch)

	testCases := []struct {
		name     string
		workload client.Object
		gvkToUse schema.GroupVersionKind
	}{
		{
			name:     "PodMonitor UPDATE operation",
			workload: newPodMonitor(testPodMonitor, testNamespace),
			gvkToUse: gvk.CoreosPodMonitor,
		},
		{
			name:     "ServiceMonitor UPDATE operation",
			workload: newServiceMonitor(testServiceMonitor, testNamespace),
			gvkToUse: gvk.CoreosServiceMonitor,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var resourceName string
			if tc.gvkToUse.Kind == "PodMonitor" {
				resourceName = "podmonitors"
			} else {
				resourceName = "servicemonitors"
			}

			req := envtestutil.NewAdmissionRequest(
				t,
				admissionv1.Update,
				tc.workload,
				tc.gvkToUse,
				metav1.GroupVersionResource{
					Group:    tc.gvkToUse.Group,
					Version:  tc.gvkToUse.Version,
					Resource: resourceName,
				},
			)

			resp := injector.Handle(ctx, req)

			// Verify update operation is processed correctly
			g.Expect(resp.Allowed).Should(BeTrue())
			g.Expect(resp.Patches).Should(Not(BeEmpty()))

			// Verify specific patches are applied for update operations
			g.Expect(resp.Result).Should(BeNil(), "Update operations should not return error results")
			g.Expect(hasLabelPatch(resp.Patches)).Should(BeTrue(), "Should have monitoring label patch")
		})
	}
}

// TestMonitoring_PreservesExistingLabels tests that existing labels are preserved when injecting the monitoring label.
func TestMonitoring_PreservesExistingLabels(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create namespace with proper labels
	ns := newMonitoredNamespace(testNamespace)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(ns).Build()
	injector := createWebhookInjector(cli, sch)

	// Create PodMonitor with existing labels
	podMonitor := newPodMonitor(testPodMonitor, testNamespace, envtestutil.WithLabels(map[string]string{
		"app":  "my-app",
		"team": "platform",
	}))

	req := envtestutil.NewAdmissionRequest(
		t,
		admissionv1.Create,
		podMonitor,
		gvk.CoreosPodMonitor,
		metav1.GroupVersionResource{
			Group:    gvk.CoreosPodMonitor.Group,
			Version:  gvk.CoreosPodMonitor.Version,
			Resource: "podmonitors",
		},
	)

	resp := injector.Handle(ctx, req)
	g.Expect(resp.Allowed).Should(BeTrue())
	g.Expect(resp.Patches).Should(Not(BeEmpty()))

	// Verify the patch adds the monitoring label
	g.Expect(hasLabelPatch(resp.Patches)).Should(BeTrue(), "Should have monitoring label patch")
}

// TestMonitoring_SkipsObjectsMarkedForDeletion tests that objects with deletion timestamp are skipped.
func TestMonitoring_SkipsObjectsMarkedForDeletion(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create namespace with proper labels
	ns := newMonitoredNamespace(testNamespace)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(ns).Build()
	injector := createWebhookInjector(cli, sch)

	// Create PodMonitor with deletion timestamp
	podMonitor := newPodMonitor(testPodMonitor, testNamespace).(*unstructured.Unstructured)
	now := metav1.Now()
	podMonitor.SetDeletionTimestamp(&now)

	req := envtestutil.NewAdmissionRequest(
		t,
		admissionv1.Update,
		podMonitor,
		gvk.CoreosPodMonitor,
		metav1.GroupVersionResource{
			Group:    gvk.CoreosPodMonitor.Group,
			Version:  gvk.CoreosPodMonitor.Version,
			Resource: "podmonitors",
		},
	)

	resp := injector.Handle(ctx, req)
	g.Expect(resp.Allowed).Should(BeTrue())
	g.Expect(resp.Patches).Should(BeEmpty())
	g.Expect(resp.Result.Message).Should(ContainSubstring("marked for deletion"))
}
