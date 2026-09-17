package dashboard_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dscv3 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	v2webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v2"
	v3webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

const dashboardIntegrationDSCName = "dashboard-conversion"

func TestDashboardVersionedConversion(t *testing.T) {
	g := NewWithT(t)
	ctx, env, teardown := envtestutil.SetupEnvAndClient(t,
		[]envt.RegisterWebhooksFn{v2webhook.RegisterWebhooks, v3webhook.RegisterWebhooks},
		nil,
		envtestutil.DefaultWebhookTimeout,
	)
	t.Cleanup(teardown)

	g.Expect(env.ConfigureCRDConversion(ctx, "datascienceclusters.datasciencecluster.opendatahub.io")).To(Succeed())

	cli := env.Client()
	key := types.NamespacedName{Name: dashboardIntegrationDSCName}

	t.Run("v2 to v3", func(t *testing.T) {
		deleteDashboardDSC(t, cli, key)

		g := NewWithT(t)
		g.Expect(cli.Create(ctx, &dscv2.DataScienceCluster{
			ObjectMeta: metav1.ObjectMeta{Name: dashboardIntegrationDSCName},
			Spec: dscv2.DataScienceClusterSpec{
				Components: dscv2.Components{
					Dashboard: componentApi.DSCDashboardV2{
						ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
						DashboardCommonSpecV2: componentApi.DashboardCommonSpecV2{
							MaaSConsumerPortal: componentApi.MaaSConsumerPortalSpec{
								ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Removed},
							},
						},
					},
				},
			},
		})).To(Succeed())

		actual := &dscv3.DataScienceCluster{}
		g.Eventually(func() error { return cli.Get(ctx, key, actual) }).Should(Succeed())
		g.Expect(actual.Spec.Components.Dashboard.Standard.ManagementState).To(Equal(operatorv1.Managed))
		g.Expect(actual.Spec.Components.Dashboard.MaaSPortal.ManagementState).To(Equal(operatorv1.Removed))
	})

	t.Run("v3 to v2", func(t *testing.T) {
		deleteDashboardDSC(t, cli, key)

		g := NewWithT(t)
		g.Expect(cli.Create(ctx, &dscv3.DataScienceCluster{
			ObjectMeta: metav1.ObjectMeta{Name: dashboardIntegrationDSCName},
			Spec: dscv3.DataScienceClusterSpec{
				Components: dscv3.Components{
					Dashboard: componentApi.DSCDashboard{
						DashboardCommonSpec: componentApi.DashboardCommonSpec{
							Standard: componentApi.DashboardStandardSpec{
								ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Removed},
							},
							MaaSPortal: componentApi.DashboardMaaSPortalSpec{
								ManagementSpec: common.ManagementSpec{ManagementState: operatorv1.Managed},
							},
						},
					},
				},
			},
		})).To(Succeed())

		actual := &dscv2.DataScienceCluster{}
		g.Eventually(func() error { return cli.Get(ctx, key, actual) }).Should(Succeed())
		g.Expect(actual.Spec.Components.Dashboard.ManagementState).To(Equal(operatorv1.Removed))
		g.Expect(actual.Spec.Components.Dashboard.MaaSConsumerPortal.ManagementState).To(Equal(operatorv1.Managed))
	})
}

func deleteDashboardDSC(t *testing.T, cli client.Client, key types.NamespacedName) {
	t.Helper()

	g := NewWithT(t)
	object := &dscv2.DataScienceCluster{ObjectMeta: metav1.ObjectMeta{Name: key.Name}}
	g.Eventually(func() error {
		if err := cli.Get(t.Context(), client.ObjectKeyFromObject(object), object); err != nil {
			return err
		}
		return cli.Delete(t.Context(), object)
	}).Should(MatchError(k8serr.IsNotFound, "IsNotFound"))
}
