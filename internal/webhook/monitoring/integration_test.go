package monitoring_test

import (
	"fmt"
	"testing"

	"github.com/rs/xid"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	monitoringwebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/monitoring"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

// TestMonitoringWebhook_LabelInjection tests the end-to-end webhook integration for monitoring resources.
// This is a smoke test to verify the webhook is registered and functioning.
// Detailed logic testing (edge cases, label preservation, etc.) is done in unit tests.
func TestMonitoringWebhook_LabelInjection(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name         string
		resourceName string
		createFunc   func(name, namespace string, opts ...envtestutil.ObjectOption) client.Object
	}{
		{
			name:         "PodMonitor",
			resourceName: testPodMonitor,
			createFunc:   newPodMonitor,
		},
		{
			name:         "ServiceMonitor",
			resourceName: testServiceMonitor,
			createFunc:   newServiceMonitor,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			ctx, env, teardown := envtestutil.SetupEnvAndClientWithCRDs(
				t,
				[]envt.RegisterWebhooksFn{monitoringwebhook.RegisterWebhooks},
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

			// Create Monitoring CR (required for webhook to inject labels)
			monitoringCR := newMonitoringCR()
			g.Expect(env.Client().Create(ctx, monitoringCR)).To(Succeed())

			// Create monitor resource using helper - webhook should inject monitoring label
			monitor := tc.createFunc(tc.resourceName, ns)

			g.Expect(env.Client().Create(ctx, monitor)).To(Succeed())

			// Verify monitoring label was injected by webhook using helper
			g.Expect(hasMonitoringLabel(monitor)).Should(BeTrue())
		})
	}
}

// TestMonitoringWebhook_Update tests that the webhook respects existing labels on UPDATE.
// When a monitoring label already exists (from CREATE), UPDATE should preserve it.
// This verifies the webhook doesn't overwrite user-set label values.
func TestMonitoringWebhook_Update(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	ctx, env, teardown := envtestutil.SetupEnvAndClientWithCRDs(
		t,
		[]envt.RegisterWebhooksFn{monitoringwebhook.RegisterWebhooks},
		[]envt.RegisterControllersFn{},
		envtestutil.DefaultWebhookTimeout,
		envtestutil.WithPodMonitor(),
		envtestutil.WithServiceMonitor(),
	)
	defer teardown()

	// Create test namespace with monitoring enabled using helper
	ns := fmt.Sprintf("test-ns-%s", xid.New().String())
	testNamespace := newMonitoredNamespace(ns)
	g.Expect(env.Client().Create(ctx, testNamespace)).To(Succeed())

	// Create Monitoring CR (required for webhook to check monitoring is enabled)
	monitoringCR := newMonitoringCR()
	g.Expect(env.Client().Create(ctx, monitoringCR)).To(Succeed())

	// Create PodMonitor with monitoring label explicitly set to "false"
	// Even though namespace is monitored, webhook should respect this value
	podMonitor := newPodMonitor("test-podmonitor-update", ns)
	podMonitorUnstructured, ok := podMonitor.(*unstructured.Unstructured)
	g.Expect(ok).Should(BeTrue(), "podMonitor should be *unstructured.Unstructured")

	// Set monitoring label to "false" before creation
	labels := podMonitorUnstructured.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[monitoringLabelKey] = "false"
	podMonitorUnstructured.SetLabels(labels)

	g.Expect(env.Client().Create(ctx, podMonitor)).To(Succeed())

	// Verify webhook respected the "false" value (didn't overwrite it)
	currentLabels := podMonitorUnstructured.GetLabels()
	g.Expect(currentLabels[monitoringLabelKey]).Should(Equal("false"))

	// Update the PodMonitor spec
	spec := map[string]interface{}{
		"selector": map[string]interface{}{
			"matchLabels": map[string]interface{}{
				"app": "test",
			},
		},
	}
	err := unstructured.SetNestedMap(podMonitorUnstructured.Object, spec, "spec")
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(env.Client().Update(ctx, podMonitor)).To(Succeed())

	// Verify webhook still respects the "false" value on UPDATE
	updatedLabels := podMonitorUnstructured.GetLabels()
	g.Expect(updatedLabels[monitoringLabelKey]).Should(Equal("false"))
}

// TestMonitoringWebhook_NamespaceNotMonitored verifies webhook doesn't inject
// when namespace lacks monitoring label.
// This ensures the webhook only acts on explicitly monitored namespaces.
func TestMonitoringWebhook_NamespaceNotMonitored(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	ctx, env, teardown := envtestutil.SetupEnvAndClientWithCRDs(
		t,
		[]envt.RegisterWebhooksFn{monitoringwebhook.RegisterWebhooks},
		[]envt.RegisterControllersFn{},
		envtestutil.DefaultWebhookTimeout,
		envtestutil.WithPodMonitor(),
		envtestutil.WithServiceMonitor(),
	)
	defer teardown()

	// Create namespace WITHOUT monitoring label
	ns := fmt.Sprintf("test-ns-%s", xid.New().String())
	testNamespace := envtestutil.NewNamespace(ns, map[string]string{})
	g.Expect(env.Client().Create(ctx, testNamespace)).To(Succeed())

	// Create PodMonitor without monitoring label
	podMonitor := newPodMonitor(testPodMonitor, ns)
	g.Expect(env.Client().Create(ctx, podMonitor)).To(Succeed())

	// Verify NO monitoring label was injected
	g.Expect(hasMonitoringLabel(podMonitor)).Should(BeFalse())

	// Create ServiceMonitor without monitoring label
	serviceMonitor := newServiceMonitor(testServiceMonitor, ns)
	g.Expect(env.Client().Create(ctx, serviceMonitor)).To(Succeed())

	// Verify NO monitoring label was injected
	g.Expect(hasMonitoringLabel(serviceMonitor)).Should(BeFalse())
}
