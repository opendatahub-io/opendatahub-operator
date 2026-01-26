package monitoring_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/rs/xid"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

// TestMonitoringWebhook_PodMonitor tests the end-to-end webhook integration for PodMonitor.
// This is an integration test to verify the webhook works end-to-end in a real Kubernetes environment.
func TestMonitoringWebhook_PodMonitor(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		test func(g Gomega, ctx context.Context, k8sClient client.Client, ns string)
	}{
		{
			name: "podmonitor - webhook injects monitoring label",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				// Create PodMonitor - webhook should inject monitoring label
				podMonitor := newPodMonitor(testPodMonitor, ns)
				g.Expect(k8sClient.Create(ctx, podMonitor)).To(Succeed())

				// Fetch the created PodMonitor from the API server to see webhook mutations
				createdPodMonitor := newPodMonitor(testPodMonitor, ns)
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      testPodMonitor,
					Namespace: ns,
				}, createdPodMonitor)).To(Succeed())

				// Verify monitoring label was injected by webhook
				g.Expect(hasMonitoringLabel(createdPodMonitor)).Should(BeTrue())
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			ctx, env, teardown := envtestutil.SetupEnvAndClientWithCRDs(
				t,
				[]envt.RegisterWebhooksFn{envtestutil.RegisterWebhooks},
				[]envt.RegisterControllersFn{},
				envtestutil.DefaultWebhookTimeout,
				envtestutil.WithPodMonitor(),
				envtestutil.WithServiceMonitor(),
			)
			defer teardown()

			// Create test namespace with monitoring enabled
			ns := fmt.Sprintf("test-ns-%s", xid.New().String())
			testNamespace := envtestutil.NewNamespace(ns, map[string]string{
				monitoringLabelKey: monitoringLabelValue,
			})
			g.Expect(env.Client().Create(ctx, testNamespace)).To(Succeed())

			// Run the specific test case
			tc.test(g, ctx, env.Client(), ns)
		})
	}
}

// TestMonitoringWebhook_ServiceMonitor tests the end-to-end webhook integration for ServiceMonitor.
// This is an integration test to verify the webhook works end-to-end in a real Kubernetes environment.
func TestMonitoringWebhook_ServiceMonitor(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		test func(g Gomega, ctx context.Context, k8sClient client.Client, ns string)
	}{
		{
			name: "servicemonitor - webhook injects monitoring label",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				// Create ServiceMonitor - webhook should inject monitoring label
				serviceMonitor := newServiceMonitor(testServiceMonitor, ns)
				g.Expect(k8sClient.Create(ctx, serviceMonitor)).To(Succeed())

				// Fetch the created ServiceMonitor from the API server to see webhook mutations
				createdServiceMonitor := newServiceMonitor(testServiceMonitor, ns)
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      testServiceMonitor,
					Namespace: ns,
				}, createdServiceMonitor)).To(Succeed())

				// Verify monitoring label was injected by webhook
				g.Expect(hasMonitoringLabel(createdServiceMonitor)).Should(BeTrue())
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			ctx, env, teardown := envtestutil.SetupEnvAndClientWithCRDs(
				t,
				[]envt.RegisterWebhooksFn{envtestutil.RegisterWebhooks},
				[]envt.RegisterControllersFn{},
				envtestutil.DefaultWebhookTimeout,
				envtestutil.WithPodMonitor(),
				envtestutil.WithServiceMonitor(),
			)
			defer teardown()

			// Create test namespace with monitoring enabled
			ns := fmt.Sprintf("test-ns-%s", xid.New().String())
			testNamespace := envtestutil.NewNamespace(ns, map[string]string{
				monitoringLabelKey: monitoringLabelValue,
			})
			g.Expect(env.Client().Create(ctx, testNamespace)).To(Succeed())

			// Run the specific test case
			tc.test(g, ctx, env.Client(), ns)
		})
	}
}

// TestMonitoringWebhook_Update tests that the webhook is called on UPDATE operations.
// This verifies the webhook handles updates correctly in a real Kubernetes environment.
func TestMonitoringWebhook_Update(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		test func(g Gomega, ctx context.Context, k8sClient client.Client, ns string)
	}{
		{
			name: "podmonitor - update operation preserves monitoring label",
			test: func(g Gomega, ctx context.Context, k8sClient client.Client, ns string) {
				// Create PodMonitor - webhook should inject monitoring label
				podMonitor := newPodMonitor("test-podmonitor-update", ns)
				g.Expect(k8sClient.Create(ctx, podMonitor)).To(Succeed())

				// Fetch the created PodMonitor from the API server to see webhook mutations
				createdPodMonitor := newPodMonitor("test-podmonitor-update", ns)
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      "test-podmonitor-update",
					Namespace: ns,
				}, createdPodMonitor)).To(Succeed())

				// Verify monitoring label was injected on CREATE
				g.Expect(hasMonitoringLabel(createdPodMonitor)).Should(BeTrue())

				// Update the PodMonitor - webhook should be called again
				testUpdateOperationForMonitor(g, ctx, k8sClient, ns, "test-podmonitor-update")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			ctx, env, teardown := envtestutil.SetupEnvAndClientWithCRDs(
				t,
				[]envt.RegisterWebhooksFn{envtestutil.RegisterWebhooks},
				[]envt.RegisterControllersFn{},
				envtestutil.DefaultWebhookTimeout,
				envtestutil.WithPodMonitor(),
				envtestutil.WithServiceMonitor(),
			)
			defer teardown()

			// Create test namespace with monitoring enabled
			ns := fmt.Sprintf("test-ns-%s", xid.New().String())
			testNamespace := newMonitoredNamespace(ns)
			g.Expect(env.Client().Create(ctx, testNamespace)).To(Succeed())

			// Run the specific test case
			tc.test(g, ctx, env.Client(), ns)
		})
	}
}

// testUpdateOperationForMonitor is a helper that tests update operations.
func testUpdateOperationForMonitor(g Gomega, ctx context.Context, k8sClient client.Client, ns string, name string) {
	// Fetch the monitor to update
	podMonitor := newPodMonitor(name, ns)
	g.Expect(k8sClient.Get(ctx, types.NamespacedName{
		Name:      name,
		Namespace: ns,
	}, podMonitor)).To(Succeed())

	// Update with a label change (trigger webhook)
	labels := podMonitor.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels["test-update"] = "true"
	podMonitor.SetLabels(labels)

	g.Expect(k8sClient.Update(ctx, podMonitor)).To(Succeed())

	// Verify webhook still processes updates correctly (label should still be present after update)
	g.Expect(hasMonitoringLabel(podMonitor)).Should(BeTrue())
}
