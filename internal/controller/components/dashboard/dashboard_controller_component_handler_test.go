// This file contains tests for dashboard component handler methods.
// These tests verify the core component handler interface methods.
package dashboard_test

import (
	"context"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

const fakeClientErrorMsg = "failed to create fake client: "

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
		name        string
		setupDSC    func() *dscv1.DataScienceCluster
		expectError bool
		validate    func(t *testing.T, cr *componentApi.Dashboard)
	}{
		{
			name: "ValidDSCWithManagedState",
			setupDSC: func() *dscv1.DataScienceCluster {
				return &dscv1.DataScienceCluster{
					Spec: dscv1.DataScienceClusterSpec{
						Components: dscv1.Components{
							Dashboard: componentApi.DSCDashboard{
								ManagementSpec: common.ManagementSpec{
									ManagementState: operatorv1.Managed,
								},
							},
						},
					},
				}
			},
			expectError: false,
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
			name: "ValidDSCWithUnmanagedState",
			setupDSC: func() *dscv1.DataScienceCluster {
				return &dscv1.DataScienceCluster{
					Spec: dscv1.DataScienceClusterSpec{
						Components: dscv1.Components{
							Dashboard: componentApi.DSCDashboard{
								ManagementSpec: common.ManagementSpec{
									ManagementState: operatorv1.Unmanaged,
								},
							},
						},
					},
				}
			},
			expectError: false,
			validate: func(t *testing.T, cr *componentApi.Dashboard) {
				t.Helper()
				g := NewWithT(t)
				g.Expect(cr.Name).Should(Equal(componentApi.DashboardInstanceName))
				g.Expect(cr.Annotations).Should(HaveKeyWithValue(annotations.ManagementStateAnnotation, "Unmanaged"))
			},
		},
		{
			name: "ValidDSCWithRemovedState",
			setupDSC: func() *dscv1.DataScienceCluster {
				return &dscv1.DataScienceCluster{
					Spec: dscv1.DataScienceClusterSpec{
						Components: dscv1.Components{
							Dashboard: componentApi.DSCDashboard{
								ManagementSpec: common.ManagementSpec{
									ManagementState: operatorv1.Removed,
								},
							},
						},
					},
				}
			},
			expectError: false,
			validate: func(t *testing.T, cr *componentApi.Dashboard) {
				t.Helper()
				g := NewWithT(t)
				g.Expect(cr.Name).Should(Equal(componentApi.DashboardInstanceName))
				g.Expect(cr.Annotations).Should(HaveKeyWithValue(annotations.ManagementStateAnnotation, "Removed"))
			},
		},
		{
			name: "DSCWithCustomSpec",
			setupDSC: func() *dscv1.DataScienceCluster {
				return &dscv1.DataScienceCluster{
					Spec: dscv1.DataScienceClusterSpec{
						Components: dscv1.Components{
							Dashboard: componentApi.DSCDashboard{
								ManagementSpec: common.ManagementSpec{
									ManagementState: operatorv1.Managed,
								},
								DashboardCommonSpec: componentApi.DashboardCommonSpec{
									DevFlagsSpec: common.DevFlagsSpec{
										DevFlags: &common.DevFlags{
											Manifests: []common.ManifestsConfig{
												{
													URI:        "https://example.com/manifests.tar.gz",
													ContextDir: "manifests",
													SourcePath: "/custom/path",
												},
											},
										},
									},
								},
							},
						},
					},
				}
			},
			expectError: false,
			validate: func(t *testing.T, cr *componentApi.Dashboard) {
				t.Helper()
				g := NewWithT(t)
				g.Expect(cr.Name).Should(Equal(componentApi.DashboardInstanceName))
				g.Expect(cr.Spec.DevFlags).ShouldNot(BeNil())
				g.Expect(cr.Spec.DevFlags.Manifests).Should(HaveLen(1))
				g.Expect(cr.Spec.DevFlags.Manifests[0].URI).Should(Equal("https://example.com/manifests.tar.gz"))
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			handler := &dashboard.ComponentHandler{}
			dsc := tc.setupDSC()

			cr := handler.NewCRObject(dsc)

			if tc.expectError {
				g := NewWithT(t)
				g.Expect(cr).Should(BeNil())
			} else {
				g := NewWithT(t)
				g.Expect(cr).ShouldNot(BeNil())
				if tc.validate != nil {
					if dashboard, ok := cr.(*componentApi.Dashboard); ok {
						tc.validate(t, dashboard)
					}
				}
			}
		})
	}
}

func TestComponentHandlerIsEnabled(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		setupDSC func() *dscv1.DataScienceCluster
		expected bool
	}{
		{
			name: "ManagedState",
			setupDSC: func() *dscv1.DataScienceCluster {
				return &dscv1.DataScienceCluster{
					Spec: dscv1.DataScienceClusterSpec{
						Components: dscv1.Components{
							Dashboard: componentApi.DSCDashboard{
								ManagementSpec: common.ManagementSpec{
									ManagementState: operatorv1.Managed,
								},
							},
						},
					},
				}
			},
			expected: true,
		},
		{
			name: "UnmanagedState",
			setupDSC: func() *dscv1.DataScienceCluster {
				return &dscv1.DataScienceCluster{
					Spec: dscv1.DataScienceClusterSpec{
						Components: dscv1.Components{
							Dashboard: componentApi.DSCDashboard{
								ManagementSpec: common.ManagementSpec{
									ManagementState: operatorv1.Unmanaged,
								},
							},
						},
					},
				}
			},
			expected: false,
		},
		{
			name: "RemovedState",
			setupDSC: func() *dscv1.DataScienceCluster {
				return &dscv1.DataScienceCluster{
					Spec: dscv1.DataScienceClusterSpec{
						Components: dscv1.Components{
							Dashboard: componentApi.DSCDashboard{
								ManagementSpec: common.ManagementSpec{
									ManagementState: operatorv1.Removed,
								},
							},
						},
					},
				}
			},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			handler := &dashboard.ComponentHandler{}
			dsc := tc.setupDSC()

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
		setupRR        func() *odhtypes.ReconciliationRequest
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
			rr := tc.setupRR()

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
func setupNilClientRR() *odhtypes.ReconciliationRequest {
	return &odhtypes.ReconciliationRequest{
		Client:   nil,
		Instance: &dscv1.DataScienceCluster{},
		DSCI: &dsciv1.DSCInitialization{
			Spec: dsciv1.DSCInitializationSpec{
				ApplicationsNamespace: testNamespace,
			},
		},
	}
}

func setupDashboardExistsRR() *odhtypes.ReconciliationRequest {
	cli, err := fakeclient.New()
	if err != nil {
		panic(fakeClientErrorMsg + err.Error())
	}
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
				URL: "https://dashboard.example.com",
			},
		},
	}
	if err := cli.Create(context.Background(), dashboard); err != nil {
		panic("Failed to create dashboard: " + err.Error())
	}

	dsc := &dscv1.DataScienceCluster{
		Spec: dscv1.DataScienceClusterSpec{
			Components: dscv1.Components{
				Dashboard: componentApi.DSCDashboard{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
				},
			},
		},
		Status: dscv1.DataScienceClusterStatus{
			InstalledComponents: make(map[string]bool),
			Components: dscv1.ComponentsStatus{
				Dashboard: componentApi.DSCDashboardStatus{},
			},
		},
	}

	return &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: dsc,
		DSCI: &dsciv1.DSCInitialization{
			Spec: dsciv1.DSCInitializationSpec{
				ApplicationsNamespace: testNamespace,
			},
		},
		Conditions: &conditions.Manager{},
	}
}

func setupDashboardNotExistsRR() *odhtypes.ReconciliationRequest {
	cli, err := fakeclient.New()
	if err != nil {
		panic(fakeClientErrorMsg + err.Error())
	}
	dsc := &dscv1.DataScienceCluster{
		Spec: dscv1.DataScienceClusterSpec{
			Components: dscv1.Components{
				Dashboard: componentApi.DSCDashboard{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
				},
			},
		},
		Status: dscv1.DataScienceClusterStatus{
			InstalledComponents: make(map[string]bool),
			Components: dscv1.ComponentsStatus{
				Dashboard: componentApi.DSCDashboardStatus{},
			},
		},
	}

	return &odhtypes.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		DSCI:       &dsciv1.DSCInitialization{},
		Conditions: &conditions.Manager{},
	}
}

func setupDashboardDisabledRR() *odhtypes.ReconciliationRequest {
	cli, err := fakeclient.New()
	if err != nil {
		panic(fakeClientErrorMsg + err.Error())
	}
	dsc := &dscv1.DataScienceCluster{
		Spec: dscv1.DataScienceClusterSpec{
			Components: dscv1.Components{
				Dashboard: componentApi.DSCDashboard{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Unmanaged,
					},
				},
			},
		},
		Status: dscv1.DataScienceClusterStatus{
			InstalledComponents: map[string]bool{
				"other-component": true,
			},
			Components: dscv1.ComponentsStatus{
				Dashboard: componentApi.DSCDashboardStatus{},
			},
		},
	}

	return &odhtypes.ReconciliationRequest{
		Client:     cli,
		Instance:   dsc,
		DSCI:       &dsciv1.DSCInitialization{},
		Conditions: &conditions.Manager{},
	}
}

func setupInvalidInstanceRR() *odhtypes.ReconciliationRequest {
	cli, err := fakeclient.New()
	if err != nil {
		panic(fakeClientErrorMsg + err.Error())
	}
	return &odhtypes.ReconciliationRequest{
		Client:   cli,
		Instance: &componentApi.Dashboard{}, // Wrong type
	}
}

func validateDashboardExists(t *testing.T, dsc *dscv1.DataScienceCluster, status metav1.ConditionStatus) {
	t.Helper()
	g := NewWithT(t)
	g.Expect(dsc.Status.InstalledComponents[dashboard.LegacyComponentNameUpstream]).Should(BeTrue())
	g.Expect(dsc.Status.Components.Dashboard.DashboardCommonStatus).ShouldNot(BeNil())
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
