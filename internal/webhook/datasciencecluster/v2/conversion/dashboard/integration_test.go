package dashboard_test

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dscApi "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
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

		actual := &dscApi.DataScienceCluster{}
		g.Eventually(func() error { return cli.Get(ctx, key, actual) }).Should(Succeed())
		g.Expect(actual.Spec.Components.Dashboard.Standard.ManagementState).To(Equal(operatorv1.Managed))
		g.Expect(actual.Spec.Components.Dashboard.MaaSPortal.ManagementState).To(Equal(operatorv1.Removed))
	})

	t.Run("v3 to v2", func(t *testing.T) {
		deleteDashboardDSC(t, cli, key)

		g := NewWithT(t)
		g.Expect(cli.Create(ctx, &dscApi.DataScienceCluster{
			ObjectMeta: metav1.ObjectMeta{Name: dashboardIntegrationDSCName},
			Spec: dscApi.DataScienceClusterSpec{
				Components: dscApi.Components{
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

	t.Run("portal management state is not defaulted", func(t *testing.T) {
		g := NewWithT(t)
		dynamicClient := env.DynamicClient()
		for _, version := range []struct {
			name         string
			groupVersion schema.GroupVersion
			portalField  string
		}{
			{name: "v2", groupVersion: dscv2.GroupVersion, portalField: "maasConsumerPortal"},
			{name: "v3", groupVersion: dscApi.GroupVersion, portalField: "maasPortal"},
		} {
			for _, portal := range []struct {
				name  string
				value map[string]any
			}{
				{name: "omitted"},
				{name: "empty", value: map[string]any{}},
			} {
				deleteDashboardDSC(t, cli, key)
				name := dashboardIntegrationDSCName
				dashboard := map[string]any{}
				if portal.value != nil {
					dashboard[version.portalField] = portal.value
				}
				object := &unstructured.Unstructured{Object: map[string]any{
					"apiVersion": version.groupVersion.String(),
					"kind":       "DataScienceCluster",
					"metadata":   map[string]any{"name": name},
					"spec": map[string]any{"components": map[string]any{
						"dashboard": dashboard,
					}},
				}}
				resource := dynamicClient.Resource(version.groupVersion.WithResource("datascienceclusters"))
				created, err := resource.Create(ctx, object, metav1.CreateOptions{})
				g.Expect(err).NotTo(HaveOccurred(), "%s/%s", version.name, portal.name)
				_, found, err := unstructured.NestedFieldNoCopy(created.Object, "spec", "components", "dashboard", version.portalField, "managementState")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(found).To(BeFalse(), "%s/%s managementState must remain unset", version.name, portal.name)
				g.Expect(resource.Delete(ctx, name, metav1.DeleteOptions{})).To(Succeed())
			}
		}
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
