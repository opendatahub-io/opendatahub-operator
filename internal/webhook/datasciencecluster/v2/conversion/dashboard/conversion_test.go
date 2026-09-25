package dashboard_test

import (
	"fmt"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dscv3 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"

	. "github.com/onsi/gomega"
)

func TestDashboardConversionV2ToV3(t *testing.T) {
	states := []operatorv1.ManagementState{"", operatorv1.Managed, operatorv1.Removed}
	for _, standardState := range states {
		for _, portalState := range states {
			name := fmt.Sprintf("standard=%q/maasPortal=%q", standardState, portalState)
			t.Run(name, func(t *testing.T) {
				g := NewWithT(t)
				source := &dscv2.DataScienceCluster{}
				source.Spec.Components.Dashboard.ManagementState = standardState
				source.Spec.Components.Dashboard.MaaSConsumerPortal.ManagementState = portalState
				source.Status.Components.Dashboard.ManagementState = standardState
				source.Status.Components.MaaSConsumerPortal.ManagementState = portalState

				hub := &dscv3.DataScienceCluster{}
				g.Expect(source.ConvertTo(hub)).To(Succeed())

				g.Expect(hub.Spec.Components.Dashboard.Standard.ManagementState).To(Equal(standardState))
				g.Expect(hub.Spec.Components.Dashboard.MaaSPortal.ManagementState).To(Equal(portalState))
				g.Expect(hub.Status.Components.Dashboard.ManagementState).To(Equal(standardState))
				g.Expect(hub.Status.Components.MaaSConsumerPortal.ManagementState).To(Equal(portalState))
			})
		}
	}
}

func TestDashboardConversionV3ToV2(t *testing.T) {
	states := []operatorv1.ManagementState{"", operatorv1.Managed, operatorv1.Removed}
	for _, standardState := range states {
		for _, portalState := range states {
			name := fmt.Sprintf("standard=%q/maasPortal=%q", standardState, portalState)
			t.Run(name, func(t *testing.T) {
				g := NewWithT(t)
				source := &dscv3.DataScienceCluster{
					Spec: dscv3.DataScienceClusterSpec{
						Components: dscv3.Components{
							Dashboard: componentApi.DSCDashboard{
								DashboardCommonSpec: componentApi.DashboardCommonSpec{
									Standard: componentApi.DashboardStandardSpec{
										ManagementSpec: common.ManagementSpec{ManagementState: standardState},
									},
									MaaSPortal: componentApi.DashboardMaaSPortalSpec{
										ManagementSpec: common.ManagementSpec{ManagementState: portalState},
									},
								},
							},
						},
					},
					Status: dscv3.DataScienceClusterStatus{
						Components: dscv3.ComponentsStatus{
							Dashboard: componentApi.DSCDashboardStatus{
								ManagementSpec: common.ManagementSpec{ManagementState: standardState},
							},
							MaaSConsumerPortal: componentApi.DSCMaaSConsumerPortalStatus{
								ManagementSpec: common.ManagementSpec{ManagementState: portalState},
							},
						},
					},
				}

				destination := &dscv2.DataScienceCluster{}
				g.Expect(destination.ConvertFrom(source)).To(Succeed())

				g.Expect(destination.Spec.Components.Dashboard.ManagementState).To(Equal(standardState))
				g.Expect(destination.Spec.Components.Dashboard.MaaSConsumerPortal.ManagementState).To(Equal(portalState))
				g.Expect(destination.Status.Components.Dashboard.ManagementState).To(Equal(standardState))
				g.Expect(destination.Status.Components.MaaSConsumerPortal.ManagementState).To(Equal(portalState))
			})
		}
	}
}
