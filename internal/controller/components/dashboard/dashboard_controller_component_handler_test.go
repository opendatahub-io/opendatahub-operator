// This file contains tests for dashboard component handler methods.
// These tests verify the core component handler interface methods.
//
//nolint:testpackage
package dashboard

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

const (
	managementStateAnnotation = "component.opendatahub.io/management-state"
)

func TestComponentHandlerGetName(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	handler := &ComponentHandler{}
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
				g.Expect(cr.Annotations).Should(HaveKeyWithValue(managementStateAnnotation, "Managed"))
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
				g.Expect(cr.Annotations).Should(HaveKeyWithValue(managementStateAnnotation, "Unmanaged"))
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
				g.Expect(cr.Annotations).Should(HaveKeyWithValue(managementStateAnnotation, "Removed"))
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
			handler := &ComponentHandler{}
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
			handler := &ComponentHandler{}
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
			name: "NilClient",
			setupRR: func() *odhtypes.ReconciliationRequest {
				return &odhtypes.ReconciliationRequest{
					Client:   nil,
					Instance: &dscv1.DataScienceCluster{},
				}
			},
			expectError:    true,
			expectedStatus: metav1.ConditionUnknown,
		},
		{
			name: "DashboardCRExistsAndEnabled",
			setupRR: func() *odhtypes.ReconciliationRequest {
				cli, _ := fakeclient.New()
				dashboard := &componentApi.Dashboard{
					ObjectMeta: metav1.ObjectMeta{
						Name: componentApi.DashboardInstanceName,
					},
					Status: componentApi.DashboardStatus{
						DashboardCommonStatus: componentApi.DashboardCommonStatus{
							URL: "https://dashboard.example.com",
						},
					},
				}
				err := cli.Create(t.Context(), dashboard)
				if err != nil {
					t.Fatalf("Failed to create dashboard: %v", err)
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
					Conditions: &conditions.Manager{},
				}
			},
			expectError:    false,
			expectedStatus: metav1.ConditionFalse, // Will be False if no Ready condition
			validateResult: func(t *testing.T, dsc *dscv1.DataScienceCluster, status metav1.ConditionStatus) {
				t.Helper()
				g := NewWithT(t)
				g.Expect(dsc.Status.InstalledComponents[LegacyComponentNameUpstream]).Should(BeTrue())
				g.Expect(dsc.Status.Components.Dashboard.DashboardCommonStatus).ShouldNot(BeNil())
			},
		},
		{
			name: "DashboardCRNotExists",
			setupRR: func() *odhtypes.ReconciliationRequest {
				cli, _ := fakeclient.New()
				// Test case where Dashboard CR doesn't exist but component is managed
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
					Conditions: &conditions.Manager{},
				}
			},
			expectError:    false,
			expectedStatus: metav1.ConditionFalse,
			validateResult: func(t *testing.T, dsc *dscv1.DataScienceCluster, status metav1.ConditionStatus) {
				t.Helper()
				g := NewWithT(t)
				// Verify component is not installed when CR doesn't exist
				g.Expect(dsc.Status.InstalledComponents[LegacyComponentNameUpstream]).Should(BeFalse())
				g.Expect(status).Should(Equal(metav1.ConditionFalse))
			},
		},
		{
			name: "DashboardDisabled",
			setupRR: func() *odhtypes.ReconciliationRequest {
				cli, _ := fakeclient.New()
				// Test case where Dashboard component is disabled (unmanaged)
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
					Conditions: &conditions.Manager{},
				}
			},
			expectError:    false,
			expectedStatus: metav1.ConditionUnknown,
			validateResult: func(t *testing.T, dsc *dscv1.DataScienceCluster, status metav1.ConditionStatus) {
				t.Helper()
				g := NewWithT(t)
				// Verify component is not installed when disabled
				g.Expect(dsc.Status.InstalledComponents[LegacyComponentNameUpstream]).Should(BeFalse())
				g.Expect(status).Should(Equal(metav1.ConditionUnknown))
			},
		},
		{
			name: "InvalidInstanceType",
			setupRR: func() *odhtypes.ReconciliationRequest {
				cli, _ := fakeclient.New()
				return &odhtypes.ReconciliationRequest{
					Client:   cli,
					Instance: &componentApi.Dashboard{}, // Wrong type
				}
			},
			expectError:    true,
			expectedStatus: metav1.ConditionUnknown,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			handler := &ComponentHandler{}
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
