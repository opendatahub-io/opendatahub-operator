// This file contains tests for dashboard component handler methods.
// These tests verify the core component handler interface methods.
package dashboard_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

const (
	creatingFakeClientMsg = "creating fake client"
)

func TestComponentHandlerGetName(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	handler := &dashboard.ComponentHandler{}
	name := handler.GetName()

	g.Expect(name).Should(Equal(componentApi.DashboardComponentName))
}

func TestComponentHandlerNewCRObject(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		state    operatorv1.ManagementState
		validate func(t *testing.T, cr *componentApi.Dashboard)
	}{
		{
			name:  "ValidDSCWithManagedState",
			state: operatorv1.Managed,
			validate: func(t *testing.T, cr *componentApi.Dashboard) {
				t.Helper()
				g := NewWithT(t)
				g.Expect(cr.Name).Should(Equal(componentApi.DashboardInstanceName))
				g.Expect(cr.Kind).Should(Equal(componentApi.DashboardKind))
				g.Expect(cr.APIVersion).Should(Equal(componentApi.GroupVersion.String()))
				g.Expect(cr.Annotations).Should(HaveKeyWithValue(annotations.ManagementStateAnnotation, "Managed"))
			},
		},
		{
			name:  "ValidDSCWithUnmanagedState",
			state: operatorv1.Unmanaged,
			validate: func(t *testing.T, cr *componentApi.Dashboard) {
				t.Helper()
				g := NewWithT(t)
				g.Expect(cr.Name).Should(Equal(componentApi.DashboardInstanceName))
				g.Expect(cr.Annotations).Should(HaveKeyWithValue(annotations.ManagementStateAnnotation, "Unmanaged"))
			},
		},
		{
			name:  "ValidDSCWithRemovedState",
			state: operatorv1.Removed,
			validate: func(t *testing.T, cr *componentApi.Dashboard) {
				t.Helper()
				g := NewWithT(t)
				g.Expect(cr.Name).Should(Equal(componentApi.DashboardInstanceName))
				g.Expect(cr.Annotations).Should(HaveKeyWithValue(annotations.ManagementStateAnnotation, "Removed"))
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			handler := &dashboard.ComponentHandler{}
			dsc := createDSCWithDashboard(tc.state)

			cr := handler.NewCRObject(dsc)

			g := NewWithT(t)
			g.Expect(cr).ShouldNot(BeNil())
			if tc.validate != nil {
				if dashboard, ok := cr.(*componentApi.Dashboard); ok {
					tc.validate(t, dashboard)
				}
			}
		})
	}
}

func TestComponentHandlerIsEnabled(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		state    operatorv1.ManagementState
		expected bool
	}{
		{
			name:     "ManagedState",
			state:    operatorv1.Managed,
			expected: true,
		},
		{
			name:     "UnmanagedState",
			state:    operatorv1.Unmanaged,
			expected: false,
		},
		{
			name:     "RemovedState",
			state:    operatorv1.Removed,
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			handler := &dashboard.ComponentHandler{}
			dsc := createDSCWithDashboard(tc.state)

			result := handler.IsEnabled(dsc)

			g := NewWithT(t)
			g.Expect(result).Should(Equal(tc.expected))
		})
	}
}

func TestComponentHandlerUpdateDSCStatus(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name           string
		setupRR        func(t *testing.T) *odhtypes.ReconciliationRequest
		expectError    bool
		expectedStatus metav1.ConditionStatus
		validateResult func(t *testing.T, dsc *dscv1.DataScienceCluster, status metav1.ConditionStatus)
	}{
		{
			name:           "NilClient",
			setupRR:        setupNilClientRR,
			expectError:    true,
			expectedStatus: metav1.ConditionUnknown,
		},
		{
			name:           "DashboardCRExistsAndEnabled",
			setupRR:        setupDashboardExistsRR,
			expectError:    false,
			expectedStatus: metav1.ConditionTrue,
			validateResult: validateDashboardExists,
		},
		{
			name:           "DashboardCRNotExists",
			setupRR:        setupDashboardNotExistsRR,
			expectError:    false,
			expectedStatus: metav1.ConditionFalse,
			validateResult: validateDashboardNotExists,
		},
		{
			name:           "DashboardDisabled",
			setupRR:        setupDashboardDisabledRR,
			expectError:    false,
			expectedStatus: metav1.ConditionUnknown,
			validateResult: validateDashboardDisabled,
		},
		{
			name:           "InvalidInstanceType",
			setupRR:        setupInvalidInstanceRR,
			expectError:    true,
			expectedStatus: metav1.ConditionUnknown,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			handler := &dashboard.ComponentHandler{}
			rr := tc.setupRR(t)

			status, err := handler.UpdateDSCStatus(t.Context(), rr)

			g := NewWithT(t)
			if tc.expectError {
				g.Expect(err).Should(HaveOccurred())
			} else {
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(status).Should(Equal(tc.expectedStatus))
			}

			if tc.validateResult != nil && !tc.expectError {
				dsc, ok := rr.Instance.(*dscv1.DataScienceCluster)
				if ok {
					tc.validateResult(t, dsc, status)
				}
			}
		})
	}
}

// Helper functions to reduce cognitive complexity.
func setupNilClientRR(t *testing.T) *odhtypes.ReconciliationRequest {
	t.Helper()
	return &odhtypes.ReconciliationRequest{
		Client:   nil,
		Instance: &dscv1.DataScienceCluster{},
		DSCI:     createDSCI(),
	}
}

func setupDashboardExistsRR(t *testing.T) *odhtypes.ReconciliationRequest {
	t.Helper()
	cli, err := fakeclient.New()
	require.NoError(t, err, creatingFakeClientMsg)
	dashboard := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name:      componentApi.DashboardInstanceName,
			Namespace: testNamespace,
		},
		Status: componentApi.DashboardStatus{
			Status: common.Status{
				Conditions: []common.Condition{
					{
						Type:   status.ConditionTypeReady,
						Status: metav1.ConditionTrue,
						Reason: "ComponentReady",
					},
				},
			},
			DashboardCommonStatus: componentApi.DashboardCommonStatus{
				URL: "https://odh-dashboard-test-namespace.apps.example.com",
			},
		},
	}
	require.NoError(t, cli.Create(t.Context(), dashboard), "creating dashboard")

	dsc := createDSCWithDashboard(operatorv1.Managed)

	return &odhtypes.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		DSCI:       createDSCI(),
		Conditions: &conditions.Manager{},
	}
}

func setupDashboardNotExistsRR(t *testing.T) *odhtypes.ReconciliationRequest {
	t.Helper()
	cli, err := fakeclient.New()
	require.NoError(t, err, creatingFakeClientMsg)
	dsc := createDSCWithDashboard(operatorv1.Managed)

	return &odhtypes.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		DSCI:       createDSCI(),
		Conditions: &conditions.Manager{},
	}
}

func setupDashboardDisabledRR(t *testing.T) *odhtypes.ReconciliationRequest {
	t.Helper()
	cli, err := fakeclient.New()
	require.NoError(t, err, creatingFakeClientMsg)
	dsc := createDSCWithDashboard(operatorv1.Unmanaged)

	return &odhtypes.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		DSCI:       createDSCI(),
		Conditions: &conditions.Manager{},
	}
}

func setupInvalidInstanceRR(t *testing.T) *odhtypes.ReconciliationRequest {
	t.Helper()
	cli, err := fakeclient.New()
	require.NoError(t, err, creatingFakeClientMsg)
	return &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: CreateTestDashboard(), // Wrong type
	}
}

func validateDashboardExists(t *testing.T, dsc *dscv1.DataScienceCluster, status metav1.ConditionStatus) {
	t.Helper()
	g := NewWithT(t)
	g.Expect(dsc.Status.InstalledComponents[dashboard.LegacyComponentNameUpstream]).Should(BeTrue())
	g.Expect(dsc.Status.Components.Dashboard.DashboardCommonStatus).ShouldNot(BeNil())
	expectedURL := "https://odh-dashboard-test-namespace.apps.example.com"
	g.Expect(dsc.Status.Components.Dashboard.DashboardCommonStatus.URL).To(Equal(expectedURL))
}

func validateDashboardNotExists(t *testing.T, dsc *dscv1.DataScienceCluster, status metav1.ConditionStatus) {
	t.Helper()
	g := NewWithT(t)
	g.Expect(dsc.Status.InstalledComponents[dashboard.LegacyComponentNameUpstream]).Should(BeFalse())
	g.Expect(status).Should(Equal(metav1.ConditionFalse))
}

func validateDashboardDisabled(t *testing.T, dsc *dscv1.DataScienceCluster, status metav1.ConditionStatus) {
	t.Helper()
	g := NewWithT(t)
	g.Expect(dsc.Status.InstalledComponents[dashboard.LegacyComponentNameUpstream]).Should(BeFalse())
	g.Expect(status).Should(Equal(metav1.ConditionUnknown))
}
