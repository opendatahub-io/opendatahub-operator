package e2e_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const (
	TestNamespaceName      = "tests-monitoring-injection"
	TestPodMonitorName     = "test-podmonitor"
	TestServiceMonitorName = "test-servicemonitor"
)

const (
	ODHLabelMonitoring = "opendatahub.io/monitoring"
)

// createMonitorsEnvironment sets up the namespace and monitors with specific labels.
// Pre-test cleanup: Ensures resources from previous test runs are deleted before creation.
// Post-test: Respects --deletion-policy flag (cleanup handled by framework, not this function).
func (tc *MonitoringTestCtx) createMonitorsEnvironment(t *testing.T, namespaceLabels map[string]string, monitorLabels map[string]string) {
	t.Helper()

	// Pre-test cleanup: Delete resources from previous runs if they exist (ensures clean slate)
	t.Logf("Pre-test cleanup: removing %s namespace and monitors if they exist", TestNamespaceName)
	tc.DeleteResource(
		WithMinimalObject(gvk.CoreosPodMonitor, types.NamespacedName{Name: TestPodMonitorName, Namespace: TestNamespaceName}),
		WithIgnoreNotFound(true),
		WithWaitForDeletion(true),
	)
	tc.DeleteResource(
		WithMinimalObject(gvk.CoreosServiceMonitor, types.NamespacedName{Name: TestServiceMonitorName, Namespace: TestNamespaceName}),
		WithIgnoreNotFound(true),
		WithWaitForDeletion(true),
	)
	tc.DeleteResource(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: TestNamespaceName}),
		WithIgnoreNotFound(true),
		WithWaitForDeletion(true),
	)
	t.Logf("Pre-test cleanup completed")

	// Helper to apply metadata labels if provided
	applyLabels := func(labels map[string]string) testf.TransformFn {
		return func(obj *unstructured.Unstructured) error {
			if len(labels) == 0 {
				return nil
			}
			currentLabels := obj.GetLabels()
			if currentLabels == nil {
				currentLabels = make(map[string]string)
			}
			for k, v := range labels {
				currentLabels[k] = v
			}
			obj.SetLabels(currentLabels)
			return nil
		}
	}

	// 1. Create Namespace with provided labels
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(CreateNamespaceWithLabels(TestNamespaceName, namespaceLabels)),
	)

	// 2. Create PodMonitor
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.CoreosPodMonitor, types.NamespacedName{
			Name:      TestPodMonitorName,
			Namespace: TestNamespaceName,
		}),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.selector.matchLabels = {"app": "test"}`),
			testf.Transform(`.spec.podMetricsEndpoints = [{"port": "metrics"}]`),
			applyLabels(monitorLabels),
		)),
	)

	// 3. Create ServiceMonitor
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.CoreosServiceMonitor, types.NamespacedName{
			Name:      TestServiceMonitorName,
			Namespace: TestNamespaceName,
		}),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.selector.matchLabels = {"app": "test"}`),
			testf.Transform(`.spec.endpoints = [{"port": "metrics"}]`),
			applyLabels(monitorLabels), // Apply the passed labels
		)),
	)
}

// ValidateMonitoringWebhookTestsSetup ensures monitoring is enabled and ready before webhook tests run.
// This prevents order-dependency issues if ValidateMonitoringServiceDisabled runs before webhook tests,
// leaving monitoring in Removed state. This setup test re-enables monitoring and waits for it to be ready,
// ensuring all webhook tests start from a known, managed state.
func (tc *MonitoringTestCtx) ValidateMonitoringWebhookTestsSetup(t *testing.T) {
	t.Helper()

	t.Logf("Setting up webhook tests: enabling monitoring and waiting for ready state")

	// Enable monitoring with metrics configuration
	tc.updateMonitoringConfig(
		withManagementState(operatorv1.Managed),
		tc.withMetricsConfig(),
	)

	// Wait for Monitoring CR to exist and be Ready
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: MonitoringCRName}),
		WithCondition(And(
			jq.Match(`.spec.metrics != null`),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
		)),
		WithCustomErrorMsg("Webhook tests setup: Monitoring CR should be enabled and ready"),
	)

	t.Logf("Webhook tests setup complete: monitoring is enabled and ready")
}

// ValidateMonitoringLabelValueEnforcementOnNamespace tests that the validation policy blocks invalid monitoring label values.
func (tc *MonitoringTestCtx) ValidateMonitoringLabelValueEnforcementOnNamespace(t *testing.T) {
	t.Helper()

	// Pre-test cleanup: ensure namespace doesn't exist from prior runs
	tc.DeleteResource(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: TestNamespaceName}),
		WithIgnoreNotFound(true),
		WithWaitForDeletion(true),
	)

	// Attempt to create namespace with INVALID monitoring label value (not "true" or "false")
	invalidNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: TestNamespaceName,
			Labels: map[string]string{
				ODHLabelMonitoring: "invalid-value", // Invalid!
			},
		},
	}

	// Expect this to be BLOCKED by validation policy
	err := tc.Client().Create(tc.Context(), invalidNamespace)
	tc.g.Expect(err).To(HaveOccurred(), "Validation policy should block namespace with invalid monitoring label value")
	tc.g.Expect(err.Error()).To(ContainSubstring("must be set to 'true' or 'false'"), "Error message should indicate valid values")
}

// ValidateMonitoringLabelValueEnforcementOnMonitors tests that the validation policy blocks invalid monitoring label values.
func (tc *MonitoringTestCtx) ValidateMonitoringLabelValueEnforcementOnMonitors(t *testing.T) {
	t.Helper()

	// 1. Create a valid namespace first (validation usually requires the namespace to exist)
	tc.createMonitorsEnvironment(t, nil, nil) // Creates namespace & clean monitors. We will ignore the clean monitors.

	// Define the invalid labels we want to test
	invalidLabels := map[string]string{
		ODHLabelMonitoring: "invalid-value",
	}

	// --- Test PodMonitor Validation ---

	// Define an invalid PodMonitor object locally
	invalidPodMonitor := &unstructured.Unstructured{}
	invalidPodMonitor.SetGroupVersionKind(gvk.CoreosPodMonitor)
	invalidPodMonitor.SetName("test-invalid-podmonitor")
	invalidPodMonitor.SetNamespace(TestNamespaceName)
	invalidPodMonitor.SetLabels(invalidLabels)
	// Minimal valid spec so K8s doesn't reject it for schema reasons
	invalidPodMonitor.Object["spec"] = map[string]interface{}{
		"selector": map[string]interface{}{
			"matchLabels": map[string]interface{}{"app": "test"},
		},
		"podMetricsEndpoints": []interface{}{
			map[string]interface{}{"port": "metrics"},
		},
	}

	// Attempt to create it - Expect Error
	err := tc.Client().Create(tc.Context(), invalidPodMonitor)
	tc.g.Expect(err).To(HaveOccurred(), "Validation policy should block PodMonitor with invalid monitoring label value")
	tc.g.Expect(err.Error()).To(ContainSubstring("must be set to 'true' or 'false'"), "Error message should indicate valid values for PodMonitor")

	// --- Test ServiceMonitor Validation ---

	// Define an invalid ServiceMonitor object locally
	invalidServiceMonitor := &unstructured.Unstructured{}
	invalidServiceMonitor.SetGroupVersionKind(gvk.CoreosServiceMonitor)
	invalidServiceMonitor.SetName("test-invalid-servicemonitor")
	invalidServiceMonitor.SetNamespace(TestNamespaceName)
	invalidServiceMonitor.SetLabels(invalidLabels)
	// Minimal valid spec
	invalidServiceMonitor.Object["spec"] = map[string]interface{}{
		"selector": map[string]interface{}{
			"matchLabels": map[string]interface{}{"app": "test"},
		},
		"endpoints": []interface{}{
			map[string]interface{}{"port": "metrics"},
		},
	}

	// Attempt to create it - Expect Error
	err = tc.Client().Create(tc.Context(), invalidServiceMonitor)
	tc.g.Expect(err).To(HaveOccurred(), "Validation policy should block ServiceMonitor with invalid monitoring label value")
	tc.g.Expect(err.Error()).To(ContainSubstring("must be set to 'true' or 'false'"), "Error message should indicate valid values for ServiceMonitor")
}
// ValidateWebhookSkipsNonMonitoredNamespace tests that webhook does NOT inject when namespace is not opted-in.
func (tc *MonitoringTestCtx) ValidateWebhookSkipsNonMonitoredNamespace(t *testing.T) {
	t.Helper()

	// Define namespace WITHOUT monitoring label (not opted-in)
	nsLabels := map[string]string{
		"some-other-label": "value",
	}

	tc.createMonitorsEnvironment(t, nsLabels, nil)

	// Verify PodMonitor does NOT have monitoring label injected
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.CoreosPodMonitor, types.NamespacedName{
			Name:      TestPodMonitorName,
			Namespace: TestNamespaceName,
		}),
		WithCondition(jq.Match(`.metadata.labels."opendatahub.io/monitoring" == null`)),
		WithCustomErrorMsg("Webhook should NOT inject monitoring label when namespace is not opted-in (PodMonitor)"),
	)

	// Verify ServiceMonitor does NOT have monitoring label injected
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.CoreosServiceMonitor, types.NamespacedName{
			Name:      TestServiceMonitorName,
			Namespace: TestNamespaceName,
		}),
		WithCondition(jq.Match(`.metadata.labels."opendatahub.io/monitoring" == null`)),
		WithCustomErrorMsg("Webhook should NOT inject monitoring label when namespace is not opted-in (ServiceMonitor)"),
	)
}

// ValidateWebhookSkipsExplicitlyOptedOutNamespace tests that webhook does NOT inject when namespace explicitly opts out with monitoring=false.
func (tc *MonitoringTestCtx) ValidateWebhookSkipsExplicitlyOptedOutNamespace(t *testing.T) {
	t.Helper()

	// Define namespace WITH monitoring=false (explicitly opted-out)
	nsLabels := map[string]string{
		ODHLabelMonitoring: "false",
	}

	tc.createMonitorsEnvironment(t, nsLabels, nil)

	// Verify PodMonitor does NOT have monitoring label injected
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.CoreosPodMonitor, types.NamespacedName{
			Name:      TestPodMonitorName,
			Namespace: TestNamespaceName,
		}),
		WithCondition(jq.Match(`.metadata.labels."opendatahub.io/monitoring" == null`)),
		WithCustomErrorMsg("Webhook should NOT inject monitoring label when namespace explicitly has monitoring=false (PodMonitor)"),
	)

	// Verify ServiceMonitor does NOT have monitoring label injected
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.CoreosServiceMonitor, types.NamespacedName{
			Name:      TestServiceMonitorName,
			Namespace: TestNamespaceName,
		}),
		WithCondition(jq.Match(`.metadata.labels."opendatahub.io/monitoring" == null`)),
		WithCustomErrorMsg("Webhook should NOT inject monitoring label when namespace explicitly has monitoring=false (ServiceMonitor)"),
	)
}

// ValidateWebhookRespectsUserOptOut tests that webhook respects user's explicit monitoring=false on monitors.
func (tc *MonitoringTestCtx) ValidateWebhookRespectsUserOptOut(t *testing.T) {
	t.Helper()

	// Namespace is opted-in
	nsLabels := map[string]string{
		ODHLabelMonitoring: "true",
	}

	// User explicitly sets monitoring=false on monitors (opt-out override)
	userOptOutLabels := map[string]string{
		ODHLabelMonitoring: "false",
	}

	tc.createMonitorsEnvironment(t, nsLabels, userOptOutLabels)

	// Verify PodMonitor STILL has monitoring=false (webhook respects user choice)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.CoreosPodMonitor, types.NamespacedName{
			Name:      TestPodMonitorName,
			Namespace: TestNamespaceName,
		}),
		WithCondition(jq.Match(`.metadata.labels."opendatahub.io/monitoring" == "false"`)),
		WithCustomErrorMsg("Webhook should respect user's explicit monitoring=false on PodMonitor"),
	)

	// Verify ServiceMonitor STILL has monitoring=false (webhook respects user choice)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.CoreosServiceMonitor, types.NamespacedName{
			Name:      TestServiceMonitorName,
			Namespace: TestNamespaceName,
		}),
		WithCondition(jq.Match(`.metadata.labels."opendatahub.io/monitoring" == "false"`)),
		WithCustomErrorMsg("Webhook should respect user's explicit monitoring=false on ServiceMonitor"),
	)
}

// ValidateWebhookIdempotency tests that webhook doesn't modify monitors that already have monitoring=true.
func (tc *MonitoringTestCtx) ValidateWebhookIdempotency(t *testing.T) {
	t.Helper()

	// Namespace is opted-in
	nsLabels := map[string]string{
		ODHLabelMonitoring: "true",
	}

	// Monitor already has monitoring=true
	existingLabels := map[string]string{
		ODHLabelMonitoring: "true",
	}

	tc.createMonitorsEnvironment(t, nsLabels, existingLabels)

	// Verify PodMonitor STILL has monitoring=true (webhook is idempotent)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.CoreosPodMonitor, types.NamespacedName{
			Name:      TestPodMonitorName,
			Namespace: TestNamespaceName,
		}),
		WithCondition(jq.Match(`.metadata.labels."opendatahub.io/monitoring" == "true"`)),
		WithCustomErrorMsg("Webhook should be idempotent - PodMonitor already has monitoring=true"),
	)

	// Verify ServiceMonitor STILL has monitoring=true (webhook is idempotent)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.CoreosServiceMonitor, types.NamespacedName{
			Name:      TestServiceMonitorName,
			Namespace: TestNamespaceName,
		}),
		WithCondition(jq.Match(`.metadata.labels."opendatahub.io/monitoring" == "true"`)),
		WithCustomErrorMsg("Webhook should be idempotent - ServiceMonitor already has monitoring=true"),
	)
}

// ValidateWebhookSkipsWhenMonitoringDisabled tests that webhook does not inject labels when monitoring is disabled.
// This test verifies that the webhook checks whether the Monitoring CR exists and monitoring is enabled
// before performing label injection. When monitoring is disabled (managementState: Removed),
// the webhook should not inject labels even if the namespace has opendatahub.io/monitoring=true.
func (tc *MonitoringTestCtx) ValidateWebhookSkipsWhenMonitoringDisabled(t *testing.T) {
	t.Helper()

	t.Logf("Disabling monitoring to test webhook behavior when monitoring is disabled")

	// Step 1: Disable monitoring
	tc.updateMonitoringConfig(withManagementState(operatorv1.Removed))

	// Step 2: Verify Monitoring CR is deleted
	tc.EnsureResourceGone(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: MonitoringCRName}),
	)

	t.Logf("Monitoring disabled and Monitoring CR deleted, proceeding to test webhook behavior")

	// Step 3: Create namespace with monitoring=true label (would normally trigger injection)
	nsLabels := map[string]string{
		ODHLabelMonitoring: "true",
	}

	// Step 4: Create monitors WITHOUT the opendatahub.io/monitoring label
	// The webhook should NOT inject the label because monitoring is disabled
	noLabels := map[string]string{} // No monitoring label on the monitor itself

	tc.createMonitorsEnvironment(t, nsLabels, noLabels)

	// Step 5: Verify PodMonitor does NOT have monitoring=true label injected
	// Fetch the resource once and assert immediately (don't wait with Eventually)
	podMonitor := tc.FetchResource(
		WithMinimalObject(gvk.CoreosPodMonitor, types.NamespacedName{
			Name:      TestPodMonitorName,
			Namespace: TestNamespaceName,
		}),
	)
	tc.g.Expect(podMonitor.GetLabels()).NotTo(HaveKey("opendatahub.io/monitoring"),
		"Webhook should NOT inject monitoring=true when monitoring is disabled (PodMonitor)")

	// Step 6: Verify ServiceMonitor does NOT have monitoring=true label injected
	// Fetch the resource once and assert immediately (don't wait with Eventually)
	serviceMonitor := tc.FetchResource(
		WithMinimalObject(gvk.CoreosServiceMonitor, types.NamespacedName{
			Name:      TestServiceMonitorName,
			Namespace: TestNamespaceName,
		}),
	)
	tc.g.Expect(serviceMonitor.GetLabels()).NotTo(HaveKey("opendatahub.io/monitoring"),
		"Webhook should NOT inject monitoring=true when monitoring is disabled (ServiceMonitor)")

	t.Logf("Webhook correctly skipped injection when monitoring was disabled")

	// Cleanup: Re-enable monitoring for subsequent tests
	tc.updateMonitoringConfig(
		withManagementState(operatorv1.Managed),
		tc.withMetricsConfig(),
	)

	// Wait for Monitoring CR to be ready again
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: MonitoringCRName}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue)),
		WithCustomErrorMsg("Monitoring should be re-enabled after webhook disabled test"),
	)

	t.Logf("Monitoring re-enabled successfully")
}
