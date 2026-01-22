package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

// Constants for Ray tests.
const (
	RayServiceMonitorName = "kuberay-operator-metrics-monitor"
)

type RayTestCtx struct {
	*ComponentTestCtx
}

func rayTestSuite(t *testing.T) {
	t.Helper()

	ct, err := NewComponentTestCtx(t, &componentApi.Ray{})
	require.NoError(t, err)

	componentCtx := RayTestCtx{
		ComponentTestCtx: ct,
	}

	// Define test cases.
	testCases := []TestCase{
		{"Validate component enabled", componentCtx.ValidateComponentEnabled},
		{"Validate operands have OwnerReferences", componentCtx.ValidateOperandsOwnerReferences},
		{"Validate update operand resources", componentCtx.ValidateUpdateDeploymentsResources},
		{"Validate component releases", componentCtx.ValidateComponentReleases},
		{"Validate resource deletion recovery", componentCtx.ValidateAllDeletionRecovery},
		// TODO: Enable these tests once kuberay operator creates the ServiceMonitor
		// {"Validate Ray ServiceMonitor created", componentCtx.ValidateRayServiceMonitorCreated},
		// {"Validate Ray ServiceMonitor has monitoring label", componentCtx.ValidateRayServiceMonitorHasMonitoringLabel},
		// {"Validate Ray ServiceMonitor owned by Ray CR", componentCtx.ValidateRayServiceMonitorOwnership},
		// {"Validate Ray ServiceMonitor discoverable by Target Allocator", componentCtx.ValidateRayServiceMonitorDiscoverableByTargetAllocator},
		// {"Validate Ray namespace has required labels", componentCtx.ValidateRayNamespaceLabels},
	}

	// Run the test suite.
	RunTestCases(t, testCases)
}

// ValidateRayServiceMonitorCreated validates that the Ray ServiceMonitor is created when Ray is enabled.
func (tc *RayTestCtx) ValidateRayServiceMonitorCreated(t *testing.T) {
	t.Helper()

	// Verify ServiceMonitor exists in the applications namespace
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.CoreosServiceMonitor, types.NamespacedName{
			Name:      RayServiceMonitorName,
			Namespace: tc.AppsNamespace,
		}),
		WithCondition(
			And(
				jq.Match(`.spec.selector.matchLabels."app.kubernetes.io/name" == "kuberay"`),
				jq.Match(`.spec.selector.matchLabels."app.kubernetes.io/component" == "kuberay-operator"`),
				jq.Match(`.spec.endpoints | length > 0`),
				jq.Match(`.spec.endpoints[0].port == "monitoring-port"`),
				jq.Match(`.spec.endpoints[0].path == "/metrics"`),
			),
		),
		WithCustomErrorMsg("Ray ServiceMonitor should exist with correct selector and endpoints"),
	)
}

// ValidateRayServiceMonitorHasMonitoringLabel validates that the mutating webhook injected the monitoring label.
func (tc *RayTestCtx) ValidateRayServiceMonitorHasMonitoringLabel(t *testing.T) {
	t.Helper()

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.CoreosServiceMonitor, types.NamespacedName{
			Name:      RayServiceMonitorName,
			Namespace: tc.AppsNamespace,
		}),
		WithCondition(
			jq.Match(`.metadata.labels."%s" == "%s"`, labels.Monitoring, labels.True),
		),
		WithCustomErrorMsg("Ray ServiceMonitor should have opendatahub.io/monitoring=true label injected by webhook"),
	)
}

// ValidateRayServiceMonitorOwnership validates that the Ray ServiceMonitor is owned by the Ray CR.
func (tc *RayTestCtx) ValidateRayServiceMonitorOwnership(t *testing.T) {
	t.Helper()

	// Verify the ServiceMonitor has owner reference to Ray CR
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.CoreosServiceMonitor, types.NamespacedName{
			Name:      RayServiceMonitorName,
			Namespace: tc.AppsNamespace,
		}),
		WithCondition(
			And(
				jq.Match(`.metadata.ownerReferences | length >= 1`),
				jq.Match(`.metadata.ownerReferences[] | select(.kind == "Ray") | .kind == "Ray"`),
			),
		),
		WithCustomErrorMsg("Ray ServiceMonitor should be owned by Ray CR"),
	)
}

// ValidateRayServiceMonitorDiscoverableByTargetAllocator validates that the Target Allocator can discover the Ray ServiceMonitor.
func (tc *RayTestCtx) ValidateRayServiceMonitorDiscoverableByTargetAllocator(t *testing.T) {
	t.Helper()

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.CoreosServiceMonitor, types.NamespacedName{
			Name:      RayServiceMonitorName,
			Namespace: tc.AppsNamespace,
		}),
		WithCondition(
			And(
				jq.Match(`.metadata.labels."%s" == "%s"`, labels.Monitoring, labels.True),
				jq.Match(`.metadata.labels."app.kubernetes.io/name" == "kuberay"`),
				jq.Match(`.metadata.labels."app.kubernetes.io/component" == "kuberay-operator"`),
			),
		),
		WithCustomErrorMsg("Ray ServiceMonitor should have correct labels for Target Allocator discovery"),
	)
}

// ValidateRayNamespaceLabels validates that the Ray namespace has the required labels for decentralized monitoring.
func (tc *RayTestCtx) ValidateRayNamespaceLabels(t *testing.T) {
	t.Helper()

	// Verify the applications namespace has the required labels
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Namespace, types.NamespacedName{
			Name: tc.AppsNamespace,
		}),
		WithCondition(
			And(
				jq.Match(`.metadata.labels."%s" == "%s"`, labels.ODH.OwnedNamespace, labels.True),
				jq.Match(`.metadata.labels."%s" == "%s"`, labels.Monitoring, labels.True),
			),
		),
		WithCustomErrorMsg("Applications namespace should have generated-namespace and monitoring labels"),
	)
}

