package e2e_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/types"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"

	. "github.com/onsi/gomega"
)

type MonitoringTestCtx struct {
	*TestContext
}

func monitoringTestSuite(t *testing.T) {
	t.Helper()

	// Initialize the test context.
	tc, err := NewTestContext(t)
	require.NoError(t, err)

	// Create an instance of test context.
	monitoringServiceCtx := MonitoringTestCtx{
		TestContext: tc,
	}

	// Skip tests if the ManagementState is 'Removed'.
	skipIfMonitoringRemoved(t, tc)

	// Define test cases.
	testCases := []TestCase{
		{"Auto creation of Monitoring CR", monitoringServiceCtx.ValidateMonitoringCRCreation},
		{"Test Monitoring CR content", monitoringServiceCtx.ValidateMonitoringCRDefaultContent},
	}

	// Run the test suite.
	monitoringServiceCtx.RunTestCases(t, testCases)
}

// skipIfMonitoringRemoved checks if the ManagementState is 'Removed' and skips tests accordingly.
func skipIfMonitoringRemoved(t *testing.T, tc *TestContext) {
	t.Helper()

	// Retrieve DSCInitialization resource.
	dsci := tc.RetrieveDSCInitialization(tc.DSCInitializationNamespacedName)

	// Skip tests if ManagementState is 'Removed'.
	if dsci.Spec.Monitoring.ManagementState == operatorv1.Removed {
		t.Skip("Monitoring ManagementState is 'Removed', skipping all monitoring-related tests.")
	}
}

// ValidateMonitoringCRCreation ensures that exactly one Monitoring CR exists.
func (tc *MonitoringTestCtx) ValidateMonitoringCRCreation(t *testing.T) {
	t.Helper()

	// Ensure exactly one Monitoring CR exists.
	tc.EnsureExactlyOneResourceExists(gvk.Monitoring, types.NamespacedName{})
}

// ValidateMonitoringCRDefaultContent validates the default content of the Monitoring CR.
func (tc *MonitoringTestCtx) ValidateMonitoringCRDefaultContent(t *testing.T) {
	t.Helper()

	if tc.Platform == cluster.ManagedRhoai {
		// Retrieve the DSCInitialization object.
		dsci := tc.RetrieveDSCInitialization(tc.DSCInitializationNamespacedName)

		// Ensure that the Monitoring resource exists.
		monitoring := &serviceApi.Monitoring{}
		tc.RetrieveResource(gvk.Monitoring, types.NamespacedName{}, monitoring)

		// Validate that the Monitoring CR's namespace matches the DSCInitialization spec.
		tc.g.Expect(monitoring.Spec.MonitoringCommonSpec.Namespace).
			To(Equal(dsci.Spec.Monitoring.Namespace),
				"Monitoring CR's namespace mismatch: Expected namespace '%v' as per DSCInitialization, but found '%v' in Monitoring CR.",
				dsci.Spec.Monitoring.Namespace, monitoring.Spec.MonitoringCommonSpec.Namespace)
	}
}
