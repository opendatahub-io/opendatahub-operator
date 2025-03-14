package e2e_test

import (
	"fmt"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

type MonitoringTestCtx struct {
	*testContext
	testMonitoringInstance serviceApi.Monitoring
}

func monitoringTestSuite(t *testing.T) {
	t.Helper()

	tc, err := NewTestContext()
	require.NoError(t, err)

	monitoringServiceCtx := MonitoringTestCtx{
		testContext: tc,
	}

	t.Run(tc.testDsc.Name, func(t *testing.T) {
		t.Run("Auto creation of Monitoring CR", func(t *testing.T) {
			err = monitoringServiceCtx.validateMonitoringCRCreation()
			require.NoError(t, err, "error getting Auth CR")
		})
		t.Run("Test Monitoring CR content", func(t *testing.T) {
			err = monitoringServiceCtx.validateMonitoringCRDefaultContent()
			require.NoError(t, err, "unexpected content in Auth CR")
		})
		// TODO: Add Managed monitoring test suite
	})
}

func (tc *MonitoringTestCtx) validateMonitoringCRCreation() error {
	if tc.testDSCI.Spec.Monitoring.ManagementState == operatorv1.Removed {
		return nil
	}
	monitoringList := &serviceApi.MonitoringList{}
	if err := tc.testContext.customClient.List(tc.ctx, monitoringList); err != nil {
		return fmt.Errorf("unable to find Monitoring CR instance: %w", err)
	}

	switch {
	case len(monitoringList.Items) == 1:
		tc.testMonitoringInstance = monitoringList.Items[0]
		return nil
	case len(monitoringList.Items) > 1:
		return fmt.Errorf("only one Monitoring CR expected, found %v", len(monitoringList.Items))
	default:
		return nil
	}
}

func (tc *MonitoringTestCtx) validateMonitoringCRDefaultContent() error {
	if tc.platform == cluster.ManagedRhoai {
		if tc.testMonitoringInstance.Spec.MonitoringCommonSpec.Namespace != tc.testDSCI.Spec.Monitoring.Namespace {
			return fmt.Errorf("unexpected monitoring namespace reference. Expected %v, got %v", tc.testDSCI.Spec.Monitoring.Namespace,
				tc.testMonitoringInstance.Spec.MonitoringCommonSpec.Namespace)
		}
	}
	return nil
}
