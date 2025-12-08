package monitoring_test

import (
	"fmt"
	"testing"

	"github.com/rs/xid"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	monitoringwebhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/monitoring"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

// TestMonitoringWebhook_PodMonitor tests the end-to-end webhook integration for PodMonitor.
// This is a smoke test to verify the webhook is registered and functioning.
// Detailed logic testing (edge cases, label preservation, etc.) is done in unit tests.
func TestMonitoringWebhook_PodMonitor(t *testing.T) {
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
	testNamespace := envtestutil.NewNamespace(ns, map[string]string{
		dashboardLabelKey:  dashboardLabelValue,
		monitoringLabelKey: monitoringLabelValue,
	})
	g.Expect(env.Client().Create(ctx, testNamespace)).To(Succeed())

	// Create PodMonitor using helper - webhook should inject monitoring label
	podMonitor := newPodMonitor(testPodMonitor, ns)

	g.Expect(env.Client().Create(ctx, podMonitor)).To(Succeed())

	// Verify monitoring label was injected by webhook using helper
	g.Expect(hasMonitoringLabel(podMonitor)).Should(BeTrue())
}

// TestMonitoringWebhook_ServiceMonitor tests the end-to-end webhook integration for ServiceMonitor.
// This is a smoke test to verify the webhook is registered and functioning.
// Detailed logic testing (edge cases, label preservation, etc.) is done in unit tests.
func TestMonitoringWebhook_ServiceMonitor(t *testing.T) {
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
	testNamespace := envtestutil.NewNamespace(ns, map[string]string{
		dashboardLabelKey:  dashboardLabelValue,
		monitoringLabelKey: monitoringLabelValue,
	})
	g.Expect(env.Client().Create(ctx, testNamespace)).To(Succeed())

	// Create ServiceMonitor using helper - webhook should inject monitoring label
	serviceMonitor := newServiceMonitor(testServiceMonitor, ns)

	g.Expect(env.Client().Create(ctx, serviceMonitor)).To(Succeed())

	// Verify monitoring label was injected by webhook using helper
	g.Expect(hasMonitoringLabel(serviceMonitor)).Should(BeTrue())
}

// TestMonitoringWebhook_Update tests that the webhook is called on UPDATE operations.
// This verifies the webhook handles updates correctly.
// Detailed update logic testing is done in unit tests.
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

	// Create PodMonitor using helper - webhook should inject monitoring label
	podMonitor := newPodMonitor("test-podmonitor-update", ns)

	g.Expect(env.Client().Create(ctx, podMonitor)).To(Succeed())
	g.Expect(hasMonitoringLabel(podMonitor)).Should(BeTrue())

	// Update the PodMonitor - webhook should be called again
	podMonitorUnstructured := podMonitor.(*unstructured.Unstructured)
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

	// Verify webhook still processes updates correctly using helper
	g.Expect(hasMonitoringLabel(podMonitor)).Should(BeTrue())
}
