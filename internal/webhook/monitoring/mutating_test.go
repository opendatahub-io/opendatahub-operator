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
	kindPodMonitor       = "PodMonitor"
	kindServiceMonitor   = "ServiceMonitor"
	kindPod              = "Pod"
)

// Helper functions for test simplification.

// newMonitoredNamespace creates a namespace with monitoring enabled.
func newMonitoredNamespace(name string) *corev1.Namespace {
	return envtestutil.NewNamespace(name, map[string]string{
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
//
//nolint:unparam // name parameter kept for consistency with newPodMonitor and future flexibility
func newServiceMonitor(name, namespace string, opts ...envtestutil.ObjectOption) client.Object {
	serviceMonitor := resources.GvkToUnstructured(gvk.CoreosServiceMonitor)
	serviceMonitor.SetName(name)
	serviceMonitor.SetNamespace(namespace)

	for _, opt := range opts {
		opt(serviceMonitor)
	}
	return serviceMonitor
}

// Helper function to create Monitoring CR (required for webhook to inject labels).
func newMonitoringCR() client.Object {
	monitoring := resources.GvkToUnstructured(gvk.Monitoring)
	monitoring.SetName("default-monitoring")
	return monitoring
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

// TestInjector_AllowsRequests tests various scenarios where the webhook should allow requests without processing.
func TestInjector_AllowsRequests(t *testing.T) {
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
			name:      "CREATE PodMonitor - Default Behaviour",
			operation: admissionv1.Create,
			workload:  newPodMonitor(testPodMonitor, testNamespace),
			gvkToUse:  gvk.CoreosPodMonitor,
			namespace: envtestutil.NewNamespace(testNamespace, map[string]string{}),
		},
		{
			name:      "CREATE ServiceMonitor - Default Behaviour",
			operation: admissionv1.Create,
			workload:  newServiceMonitor(testServiceMonitor, testNamespace),
			gvkToUse:  gvk.CoreosServiceMonitor,
			namespace: envtestutil.NewNamespace(testNamespace, map[string]string{}),
		},
		{
			name:      "UPDATE PodMonitor - Default Behaviour",
			operation: admissionv1.Update,
			workload:  newPodMonitor(testPodMonitor, testNamespace),
			gvkToUse:  gvk.CoreosPodMonitor,
			namespace: envtestutil.NewNamespace(testNamespace, map[string]string{}),
		},
		{
			name:      "UPDATE ServiceMonitor - Default Behaviour",
			operation: admissionv1.Update,
			workload:  newServiceMonitor(testServiceMonitor, testNamespace),
			gvkToUse:  gvk.CoreosServiceMonitor,
			namespace: envtestutil.NewNamespace(testNamespace, map[string]string{}),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(tc.namespace).Build()
			injector := createWebhookInjector(cli, sch)

			var resourceName string
			if tc.gvkToUse.Kind == kindPodMonitor {
				resourceName = testPodMonitor
			} else {
				resourceName = testServiceMonitor
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

// TestInjector_DeniesWhenDecoderNotInitialized tests that requests are denied when decoder is nil.
func TestInjector_DeniesWhenDecoderNotInitialized(t *testing.T) {
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

// TestInjector_ErrorPaths tests various error conditions in the webhook.
func TestInjector_ErrorPaths(t *testing.T) {
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
			switch tc.gvkToUse.Kind {
			case kindPod:
				gvrToUse = metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
			case kindPodMonitor:
				gvrToUse = metav1.GroupVersionResource{
					Group:    tc.gvkToUse.Group,
					Version:  tc.gvkToUse.Version,
					Resource: "podmonitors",
				}
			default:
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

// TestInjector_SkipsObjectsMarkedForDeletion tests that objects with deletion timestamp are skipped.
func TestInjector_SkipsObjectsMarkedForDeletion(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create namespace with proper labels
	ns := newMonitoredNamespace(testNamespace)

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(ns).Build()
	injector := createWebhookInjector(cli, sch)

	// Create PodMonitor with deletion timestamp
	podMonitor, ok := newPodMonitor(testPodMonitor, testNamespace).(*unstructured.Unstructured)
	g.Expect(ok).Should(BeTrue(), "podMonitor should be *unstructured.Unstructured")
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

// TestInjector_PreservesExistingLabels tests that existing labels are preserved when injecting the monitoring label.
func TestInjector_PreservesExistingLabels(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	// Create namespace with monitoring label
	ns := newMonitoredNamespace(testNamespace)
	// Create Monitoring CR (required for injection to happen)
	monitoringCR := newMonitoringCR()

	cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(ns, monitoringCR).Build()
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

// TestInjector_RespectsExistingMonitoringLabel tests that webhook doesn't patch when monitoring label already exists.
func TestInjector_RespectsExistingMonitoringLabel(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	sch, ctx := setupTestEnvironment(t)

	testCases := []struct {
		name          string
		operation     admissionv1.Operation
		workload      client.Object
		gvkToUse      schema.GroupVersionKind
		namespace     *corev1.Namespace
		existingLabel string
	}{
		{
			name:      "CREATE PodMonitor - Label already true",
			operation: admissionv1.Create,
			workload: newPodMonitor(testPodMonitor, testNamespace, func(obj client.Object) {
				obj.SetLabels(map[string]string{monitoringLabelKey: "true"})
			}),
			gvkToUse:      gvk.CoreosPodMonitor,
			namespace:     newMonitoredNamespace(testNamespace),
			existingLabel: "true",
		},
		{
			name:      "CREATE PodMonitor - Label already false",
			operation: admissionv1.Create,
			workload: newPodMonitor(testPodMonitor, testNamespace, func(obj client.Object) {
				obj.SetLabels(map[string]string{monitoringLabelKey: "false"})
			}),
			gvkToUse:      gvk.CoreosPodMonitor,
			namespace:     newMonitoredNamespace(testNamespace),
			existingLabel: "false",
		},
		{
			name:      "CREATE ServiceMonitor - Label already true",
			operation: admissionv1.Create,
			workload: newServiceMonitor(testServiceMonitor, testNamespace, func(obj client.Object) {
				obj.SetLabels(map[string]string{monitoringLabelKey: "true"})
			}),
			gvkToUse:      gvk.CoreosServiceMonitor,
			namespace:     newMonitoredNamespace(testNamespace),
			existingLabel: "true",
		},
		{
			name:      "CREATE ServiceMonitor - Label already false",
			operation: admissionv1.Create,
			workload: newServiceMonitor(testServiceMonitor, testNamespace, func(obj client.Object) {
				obj.SetLabels(map[string]string{monitoringLabelKey: "false"})
			}),
			gvkToUse:      gvk.CoreosServiceMonitor,
			namespace:     newMonitoredNamespace(testNamespace),
			existingLabel: "false",
		},
		{
			name:      "UPDATE PodMonitor - Label already true",
			operation: admissionv1.Update,
			workload: newPodMonitor(testPodMonitor, testNamespace, func(obj client.Object) {
				obj.SetLabels(map[string]string{monitoringLabelKey: "true"})
			}),
			gvkToUse:      gvk.CoreosPodMonitor,
			namespace:     newMonitoredNamespace(testNamespace),
			existingLabel: "true",
		},
		{
			name:      "UPDATE PodMonitor - Label already false",
			operation: admissionv1.Update,
			workload: newPodMonitor(testPodMonitor, testNamespace, func(obj client.Object) {
				obj.SetLabels(map[string]string{monitoringLabelKey: "false"})
			}),
			gvkToUse:      gvk.CoreosPodMonitor,
			namespace:     newMonitoredNamespace(testNamespace),
			existingLabel: "false",
		},
		{
			name:      "UPDATE ServiceMonitor - Label already true",
			operation: admissionv1.Update,
			workload: newServiceMonitor(testServiceMonitor, testNamespace, func(obj client.Object) {
				obj.SetLabels(map[string]string{monitoringLabelKey: "true"})
			}),
			gvkToUse:      gvk.CoreosServiceMonitor,
			namespace:     newMonitoredNamespace(testNamespace),
			existingLabel: "true",
		},
		{
			name:      "UPDATE ServiceMonitor - Label already false",
			operation: admissionv1.Update,
			workload: newServiceMonitor(testServiceMonitor, testNamespace, func(obj client.Object) {
				obj.SetLabels(map[string]string{monitoringLabelKey: "false"})
			}),
			gvkToUse:      gvk.CoreosServiceMonitor,
			namespace:     newMonitoredNamespace(testNamespace),
			existingLabel: "false",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Create Monitoring CR (required for proper test behavior when monitoring is enabled)
			monitoringCR := newMonitoringCR()
			cli := fake.NewClientBuilder().WithScheme(sch).WithObjects(tc.namespace, monitoringCR).Build()
			injector := createWebhookInjector(cli, sch)

			var resourceName string
			if tc.gvkToUse.Kind == kindPodMonitor {
				resourceName = testPodMonitor
			} else {
				resourceName = testServiceMonitor
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
			g.Expect(resp.Allowed).Should(BeTrue(), "Request should be allowed")
			g.Expect(resp.Patches).Should(BeEmpty(), "Should not patch when monitoring label already exists with value: %s", tc.existingLabel)
		})
	}
}
