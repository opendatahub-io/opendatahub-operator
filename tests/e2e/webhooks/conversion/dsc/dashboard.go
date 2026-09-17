package dsc

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dscv3 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers"

	. "github.com/onsi/gomega" //nolint:staticcheck // Matchers use Gomega's test DSL.
)

type DashboardSuite struct {
	WebhookSuite
}

func (m DashboardSuite) run(t *testing.T) {
	t.Run("v2_v3", m.v2ToV3)
	t.Run("v3_v2", m.v3ToV2)
}

func (m DashboardSuite) v2ToV3(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Dashboard: componentApi.DSCDashboardV2{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Managed,
					},
					DashboardCommonSpecV2: componentApi.DashboardCommonSpecV2{
						MaaSConsumerPortal: componentApi.MaaSConsumerPortalSpec{
							ManagementSpec: common.ManagementSpec{
								ManagementState: operatorv1.Removed,
							},
						},
					},
				},
			},
		},
	}).Should(Succeed())

	w.GetObject(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv3.DataScienceCluster{
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{
				Dashboard: componentApi.DSCDashboard{
					DashboardCommonSpec: componentApi.DashboardCommonSpec{
						Standard: componentApi.DashboardStandardSpec{
							ManagementSpec: common.ManagementSpec{
								ManagementState: operatorv1.Managed,
							},
						},
						MaaSPortal: componentApi.DashboardMaaSPortalSpec{
							ManagementSpec: common.ManagementSpec{
								ManagementState: operatorv1.Removed,
							},
						},
					},
				},
			},
		},
	}))
}

func (m DashboardSuite) v3ToV2(t *testing.T) {
	w := m.newScenario(t)

	w.Apply(&dscv3.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
		Spec: dscv3.DataScienceClusterSpec{
			Components: dscv3.Components{
				Dashboard: componentApi.DSCDashboard{
					DashboardCommonSpec: componentApi.DashboardCommonSpec{
						Standard: componentApi.DashboardStandardSpec{
							ManagementSpec: common.ManagementSpec{
								ManagementState: operatorv1.Removed,
							},
						},
						MaaSPortal: componentApi.DashboardMaaSPortalSpec{
							ManagementSpec: common.ManagementSpec{
								ManagementState: operatorv1.Managed,
							},
						},
					},
				},
			},
		},
	}).Should(Succeed())

	w.GetObject(&dscv2.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{Name: webhookSuiteDSCName},
	}).Should(matchers.MatchPartialObject(&dscv2.DataScienceCluster{
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				Dashboard: componentApi.DSCDashboardV2{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					DashboardCommonSpecV2: componentApi.DashboardCommonSpecV2{
						MaaSConsumerPortal: componentApi.MaaSConsumerPortalSpec{
							ManagementSpec: common.ManagementSpec{
								ManagementState: operatorv1.Managed,
							},
						},
					},
				},
			},
		},
	}))
}
