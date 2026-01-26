package e2e_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"
)

const (
	TestNamespaceName      = "tests-monitoring-injection"
	TestPodMonitorName     = "test-podmonitor"
	TestServiceMonitorName = "test-servicemonitor"
)

// createMonitorsEnvironment sets up the namespace and monitors with specific labels.
// Pre-test cleanup: Ensures resources from previous test runs are deleted before creation.
// Post-test: Respects --deletion-policy flag (cleanup handled by framework, not this function).
//
//nolint:unused // Helper function for future webhook tests - will be used when webhook validation tests are added
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
