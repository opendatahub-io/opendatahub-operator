package e2e_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
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
// It automatically registers cleanup handlers using t.Cleanup.
func (tc *MonitoringTestCtx) createMonitorsEnvironment(t *testing.T, namespaceLabels map[string]string, monitorLabels map[string]string) {
	t.Helper()

	// 1. Create Namespace with provided labels
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(CreateNamespaceWithLabels(TestNamespaceName, namespaceLabels)),
	)
	t.Cleanup(func() {
		tc.DeleteResource(
			WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: TestNamespaceName}),
			WithWaitForDeletion(true),
		)
	})

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
	t.Cleanup(func() {
		tc.DeleteResource(
			WithMinimalObject(gvk.CoreosPodMonitor, types.NamespacedName{Name: TestPodMonitorName, Namespace: TestNamespaceName}),
			WithWaitForDeletion(true),
		)
	})

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
	t.Cleanup(func() {
		tc.DeleteResource(
			WithMinimalObject(gvk.CoreosServiceMonitor, types.NamespacedName{Name: TestServiceMonitorName, Namespace: TestNamespaceName}),
			WithWaitForDeletion(true),
		)
	})
}

// ValidateMonitoringLabelValueEnforcementOnNamespace tests that the validation policy blocks invalid monitoring label values.
func (tc *MonitoringTestCtx) ValidateMonitoringLabelValueEnforcementOnNamespace(t *testing.T) {
	t.Helper()

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
