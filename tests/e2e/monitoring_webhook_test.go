package e2e_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

// ValidatePodMonitorLabelInjection tests that the mutating webhook injects opendatahub.io/monitoring=true label into PodMonitors.
func (tc *MonitoringTestCtx) ValidatePodMonitorLabelInjection(t *testing.T) {
	t.Helper()

	testNamespace := "test-podmonitor-injection"

	// Create a test namespace with ODH dashboard and monitoring labels
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(CreateNamespaceWithLabels(testNamespace, map[string]string{
			"opendatahub.io/generated-namespace": "true",
			"opendatahub.io/monitoring":          "true",
		})),
	)
	defer tc.DeleteResource(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: testNamespace}),
		WithWaitForDeletion(true),
	)

	// Create PodMonitor WITHOUT the opendatahub.io/monitoring label
	podMonitorName := "test-podmonitor"
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.CoreosPodMonitor, types.NamespacedName{
			Name:      podMonitorName,
			Namespace: testNamespace,
		}),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.selector.matchLabels = {"app": "test"}`),
			testf.Transform(`.spec.podMetricsEndpoints = [{"port": "metrics"}]`),
		)),
	)
	defer tc.DeleteResource(
		WithMinimalObject(gvk.CoreosPodMonitor, types.NamespacedName{
			Name:      podMonitorName,
			Namespace: testNamespace,
		}),
		WithWaitForDeletion(true),
	)

	// Verify webhook injected the monitoring label
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.CoreosPodMonitor, types.NamespacedName{
			Name:      podMonitorName,
			Namespace: testNamespace,
		}),
		WithCondition(jq.Match(`.metadata.labels."opendatahub.io/monitoring" == "true"`)),
		WithCustomErrorMsg("Mutating webhook should inject opendatahub.io/monitoring=true label into PodMonitor"),
	)
}

// ValidateServiceMonitorLabelInjection tests that the mutating webhook injects opendatahub.io/monitoring=true label into ServiceMonitors.
func (tc *MonitoringTestCtx) ValidateServiceMonitorLabelInjection(t *testing.T) {
	t.Helper()

	testNamespace := "test-servicemonitor-injection"

	// Create a test namespace with ODH dashboard and monitoring labels
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(CreateNamespaceWithLabels(testNamespace, map[string]string{
			"opendatahub.io/generated-namespace": "true",
			"opendatahub.io/monitoring":          "true",
		})),
	)
	defer tc.DeleteResource(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: testNamespace}),
		WithWaitForDeletion(true),
	)

	// Create ServiceMonitor WITHOUT the opendatahub.io/monitoring label
	serviceMonitorName := "test-servicemonitor"
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.CoreosServiceMonitor, types.NamespacedName{
			Name:      serviceMonitorName,
			Namespace: testNamespace,
		}),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.selector.matchLabels = {"app": "test"}`),
			testf.Transform(`.spec.endpoints = [{"port": "metrics"}]`),
		)),
	)
	defer tc.DeleteResource(
		WithMinimalObject(gvk.CoreosServiceMonitor, types.NamespacedName{
			Name:      serviceMonitorName,
			Namespace: testNamespace,
		}),
		WithWaitForDeletion(true),
	)

	// Verify webhook injected the monitoring label
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.CoreosServiceMonitor, types.NamespacedName{
			Name:      serviceMonitorName,
			Namespace: testNamespace,
		}),
		WithCondition(jq.Match(`.metadata.labels."opendatahub.io/monitoring" == "true"`)),
		WithCustomErrorMsg("Mutating webhook should inject opendatahub.io/monitoring=true label into ServiceMonitor"),
	)
}

// ValidateMonitorLabelNotInjectedInNonODHNamespace tests that the webhook does NOT inject labels in non-ODH namespaces.
func (tc *MonitoringTestCtx) ValidateMonitorLabelNotInjectedInNonODHNamespace(t *testing.T) {
	t.Helper()

	testNamespace := "test-non-odh-namespace"

	// Create a test namespace WITHOUT ODH labels
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(CreateNamespaceWithLabels(testNamespace, map[string]string{})),
	)
	defer tc.DeleteResource(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: testNamespace}),
		WithWaitForDeletion(true),
	)

	// Create ServiceMonitor WITHOUT monitoring label
	serviceMonitorName := "test-servicemonitor-non-odh"
	tc.EventuallyResourceCreatedOrUpdated(
		WithMinimalObject(gvk.CoreosServiceMonitor, types.NamespacedName{
			Name:      serviceMonitorName,
			Namespace: testNamespace,
		}),
		WithMutateFunc(testf.TransformPipeline(
			testf.Transform(`.spec.selector.matchLabels = {"app": "test"}`),
			testf.Transform(`.spec.endpoints = [{"port": "metrics"}]`),
		)),
	)
	defer tc.DeleteResource(
		WithMinimalObject(gvk.CoreosServiceMonitor, types.NamespacedName{
			Name:      serviceMonitorName,
			Namespace: testNamespace,
		}),
		WithWaitForDeletion(true),
	)

	// Verify webhook did NOT inject the monitoring label
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.CoreosServiceMonitor, types.NamespacedName{
			Name:      serviceMonitorName,
			Namespace: testNamespace,
		}),
		WithCondition(jq.Match(`(.metadata.labels."opendatahub.io/monitoring" // null) == null`)),
		WithCustomErrorMsg("Webhook should NOT inject monitoring label in non-ODH namespace"),
	)
}

// ValidateMonitoringLabelValueEnforcement tests that the validation policy blocks invalid monitoring label values.
func (tc *MonitoringTestCtx) ValidateMonitoringLabelValueEnforcement(t *testing.T) {
	t.Helper()

	testNamespace := "test-invalid-label-value"

	// Attempt to create namespace with INVALID monitoring label value (not "true" or "false")
	invalidNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
			Labels: map[string]string{
				"opendatahub.io/generated-namespace": "true",
				"opendatahub.io/monitoring":          "invalid-value", // Invalid!
			},
		},
	}

	// Expect this to be BLOCKED by validation policy
	err := tc.Client().Create(tc.Context(), invalidNamespace)
	tc.g.Expect(err).To(HaveOccurred(), "Validation policy should block namespace with invalid monitoring label value")
	tc.g.Expect(err.Error()).To(ContainSubstring("must be set to 'true' or 'false'"), "Error message should indicate valid values")
}

// ValidateNamespaceMonitoringRequiresGeneratedNamespace tests that namespaces with monitoring label must have generated-namespace label.
func (tc *MonitoringTestCtx) ValidateNamespaceMonitoringRequiresGeneratedNamespace(t *testing.T) {
	t.Helper()

	testNamespace := "test-monitoring-needs-generated-ns"

	// Attempt to create namespace with monitoring label but WITHOUT generated-namespace label
	invalidNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespace,
			Labels: map[string]string{
				"opendatahub.io/monitoring": "true", // Has monitoring
				// Missing generated-namespace label!
			},
		},
	}

	// Expect this to be BLOCKED by namespace-monitoring-validation policy
	err := tc.Client().Create(tc.Context(), invalidNamespace)
	tc.g.Expect(err).To(HaveOccurred(), "Validation policy should block namespace with monitoring label but no generated-namespace label")
	tc.g.Expect(err.Error()).To(ContainSubstring("opendatahub.io/generated-namespace"), "Error message should mention generated-namespace label requirement")
}

// ValidateMonitorsCannotHaveMonitoringLabelInNonODHNamespace tests that monitors cannot have monitoring label in non-ODH namespaces.
func (tc *MonitoringTestCtx) ValidateMonitorsCannotHaveMonitoringLabelInNonODHNamespace(t *testing.T) {
	t.Helper()

	testNamespace := "test-monitor-label-blocked"

	// Create a non-ODH namespace (no dashboard label)
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(CreateNamespaceWithLabels(testNamespace, map[string]string{})),
	)
	defer tc.DeleteResource(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: testNamespace}),
		WithWaitForDeletion(true),
	)

	// Attempt to create PodMonitor WITH monitoring label in non-ODH namespace
	// This should be BLOCKED by monitors-namespace-validation policy
	invalidPodMonitor := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "monitoring.coreos.com/v1",
			"kind":       "PodMonitor",
			"metadata": map[string]interface{}{
				"name":      "invalid-podmonitor",
				"namespace": testNamespace,
				"labels": map[string]interface{}{
					"opendatahub.io/monitoring": "true",
				},
			},
			"spec": map[string]interface{}{
				"selector": map[string]interface{}{
					"matchLabels": map[string]interface{}{
						"app": "test",
					},
				},
				"podMetricsEndpoints": []interface{}{
					map[string]interface{}{
						"port": "metrics",
					},
				},
			},
		},
	}

	err := tc.Client().Create(tc.Context(), invalidPodMonitor)
	tc.g.Expect(err).To(HaveOccurred(), "Validation policy should block PodMonitor with monitoring label in non-ODH namespace")
	tc.g.Expect(k8serr.IsInvalid(err)).To(BeTrue(), "Error should be Invalid (422) from validation policy")
}
