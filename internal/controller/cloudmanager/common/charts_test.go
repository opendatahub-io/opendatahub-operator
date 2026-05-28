//nolint:testpackage // testing unexported methods
package common

import (
	"context"
	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ccmcommon "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

const testChartsPath = "/test/charts"

func getAllUnmanagedDependencies() ccmcommon.Dependencies {
	return ccmcommon.Dependencies{
		GatewayAPI:   ccmcommon.GatewayAPIDependency{ManagementPolicy: ccmcommon.Unmanaged},
		CertManager:  ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Unmanaged},
		LWS:          ccmcommon.LWSDependency{ManagementPolicy: ccmcommon.Unmanaged},
		SailOperator: ccmcommon.SailOperatorDependency{ManagementPolicy: ccmcommon.Unmanaged},
	}
}

func newFakeClient(t *testing.T, objs ...fakeclient.ClientOpts) client.Client {
	t.Helper()

	cli, err := fakeclient.New(objs...)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())

	return cli
}

func TestBuildHelmCharts(t *testing.T) {
	ctx := context.Background()
	cli := newFakeClient(t)

	expectedReleaseNames := []string{
		"gateway-api",
		"cert-manager-operator",
		"lws-operator",
		"sail-operator",
	}

	t.Run("returns all charts in order when all managed", func(t *testing.T) {
		g := NewWithT(t)

		result := BuildHelmCharts(ctx, cli, ccmcommon.Dependencies{}, testChartsPath)

		g.Expect(result.Charts).To(HaveLen(len(expectedReleaseNames)))
		for i, name := range expectedReleaseNames {
			g.Expect(result.Charts[i].ReleaseName).To(Equal(name))
			g.Expect(result.Charts[i].Chart).To(Equal(filepath.Join(testChartsPath, name)))
		}
		g.Expect(result.FilterCRs).To(BeEmpty())
	})

	t.Run("excludes unmanaged charts and preserves order", func(t *testing.T) {
		g := NewWithT(t)

		deps := getAllUnmanagedDependencies()
		deps.LWS.ManagementPolicy = ccmcommon.Managed

		result := BuildHelmCharts(ctx, cli, deps, testChartsPath)

		g.Expect(result.Charts).To(HaveLen(1))
		g.Expect(result.Charts[0].ReleaseName).To(Equal("lws-operator"))
		g.Expect(result.FilterCRs).To(BeEmpty())
		g.Expect(result.CleanupCharts).To(HaveLen(2))
	})

	t.Run("returns empty slice when all unmanaged and no CRs on cluster", func(t *testing.T) {
		g := NewWithT(t)

		deps := getAllUnmanagedDependencies()

		result := BuildHelmCharts(ctx, cli, deps, testChartsPath)

		g.Expect(result.Charts).To(BeEmpty())
		g.Expect(result.FilterCRs).To(BeEmpty())
		g.Expect(result.CleanupCharts).To(HaveLen(3))
		g.Expect(result.CleanupCharts[0].ReleaseName).To(Equal("cert-manager-operator"))
		g.Expect(result.CleanupCharts[1].ReleaseName).To(Equal("lws-operator"))
		g.Expect(result.CleanupCharts[2].ReleaseName).To(Equal("sail-operator"))
	})

	t.Run("monitor configs include policy derived from state", func(t *testing.T) {
		g := NewWithT(t)

		deps := getAllUnmanagedDependencies()
		deps.GatewayAPI.ManagementPolicy = ccmcommon.Managed

		result := BuildHelmCharts(ctx, cli, deps, testChartsPath)

		g.Expect(result.MonitorConfigs).To(HaveLen(4))
		g.Expect(result.MonitorConfigs[0].Policy).To(Equal(ccmcommon.Managed))
		g.Expect(result.MonitorConfigs[1].Policy).To(Equal(ccmcommon.Unmanaged))
		g.Expect(result.MonitorConfigs[2].Policy).To(Equal(ccmcommon.Unmanaged))
		g.Expect(result.MonitorConfigs[3].Policy).To(Equal(ccmcommon.Unmanaged))
	})

	t.Run("uses custom namespaces in chart values", func(t *testing.T) {
		g := NewWithT(t)

		deps := ccmcommon.Dependencies{
			LWS: ccmcommon.LWSDependency{
				Configuration: ccmcommon.LWSConfiguration{
					Namespace: "custom-lws-ns",
				},
			},
			SailOperator: ccmcommon.SailOperatorDependency{
				Configuration: ccmcommon.SailOperatorConfiguration{
					Namespace: "custom-sail-ns",
				},
			},
		}

		result := BuildHelmCharts(ctx, cli, deps, testChartsPath)

		g.Expect(result.Charts).To(HaveLen(4))

		certManagerChart := result.Charts[1]
		g.Expect(certManagerChart.ReleaseName).To(Equal("cert-manager-operator"))
		values, err := certManagerChart.Values(ctx)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(values).To(HaveKeyWithValue("operatorNamespace", "cert-manager-operator"))
		g.Expect(values).To(HaveKeyWithValue("operandNamespace", "cert-manager"))

		lwsChart := result.Charts[2]
		g.Expect(lwsChart.ReleaseName).To(Equal("lws-operator"))
		values, err = lwsChart.Values(ctx)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(values).To(HaveKeyWithValue("namespace", "custom-lws-ns"))

		sailChart := result.Charts[3]
		g.Expect(sailChart.ReleaseName).To(Equal("sail-operator"))
		values, err = sailChart.Values(ctx)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(values).To(HaveKeyWithValue("namespace", "custom-sail-ns"))
	})
}

func TestBuildHelmChartsPhase1(t *testing.T) {
	ctx := context.Background()

	t.Run("single dep unmanaged with CR on cluster keeps chart and adds FilterCR", func(t *testing.T) {
		tests := []struct {
			name        string
			crGVK       schema.GroupVersionKind
			crName      string
			crNamespace string
			releaseName string
		}{
			{
				name:        "sail-operator",
				crGVK:       gvk.Istio,
				crName:      "default",
				crNamespace: ccmcommon.DefaultNamespaceSailOperator,
				releaseName: "sail-operator",
			},
			{
				name:        "LWS",
				crGVK:       gvk.LeaderWorkerSetOperatorV1,
				crName:      "cluster",
				crNamespace: ccmcommon.DefaultNamespaceLWSOperator,
				releaseName: "lws-operator",
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				g := NewWithT(t)

				cr := &unstructured.Unstructured{}
				cr.SetGroupVersionKind(tc.crGVK)
				cr.SetName(tc.crName)
				if tc.crNamespace != "" {
					cr.SetNamespace(tc.crNamespace)
				}

				cli := newFakeClient(t, fakeclient.WithObjects(cr))

				deps := getAllUnmanagedDependencies()
				result := BuildHelmCharts(ctx, cli, deps, testChartsPath)

				g.Expect(result.Charts).To(HaveLen(1))
				g.Expect(result.Charts[0].ReleaseName).To(Equal(tc.releaseName))
				g.Expect(result.FilterCRs).To(HaveLen(1))
				g.Expect(result.FilterCRs[0].GVK).To(Equal(tc.crGVK))
				g.Expect(result.FilterCRs[0].Name).To(Equal(tc.crName))
			})
		}
	})

	t.Run("gateway-api skips to excluded with no operator CR", func(t *testing.T) {
		g := NewWithT(t)
		cli := newFakeClient(t)

		deps := ccmcommon.Dependencies{
			GatewayAPI: ccmcommon.GatewayAPIDependency{ManagementPolicy: ccmcommon.Unmanaged},
		}

		result := BuildHelmCharts(ctx, cli, deps, testChartsPath)

		g.Expect(result.Charts).To(HaveLen(3))
		g.Expect(result.Charts[0].ReleaseName).To(Equal("cert-manager-operator"))
		g.Expect(result.Charts[1].ReleaseName).To(Equal("lws-operator"))
		g.Expect(result.Charts[2].ReleaseName).To(Equal("sail-operator"))
		g.Expect(result.FilterCRs).To(BeEmpty())
		g.Expect(result.CleanupCharts).To(BeEmpty())
	})

	t.Run("multiple deps unmanaged with CRs keeps charts with operatorCR", func(t *testing.T) {
		g := NewWithT(t)

		istioCR := &unstructured.Unstructured{}
		istioCR.SetGroupVersionKind(gvk.Istio)
		istioCR.SetName("default")
		istioCR.SetNamespace(ccmcommon.DefaultNamespaceSailOperator)

		certManagerCR := &unstructured.Unstructured{}
		certManagerCR.SetGroupVersionKind(gvk.CertManagerV1Alpha1)
		certManagerCR.SetName("cluster")

		lwsCR := &unstructured.Unstructured{}
		lwsCR.SetGroupVersionKind(gvk.LeaderWorkerSetOperatorV1)
		lwsCR.SetName("cluster")
		lwsCR.SetNamespace(ccmcommon.DefaultNamespaceLWSOperator)

		cli := newFakeClient(t, fakeclient.WithObjects(istioCR, certManagerCR, lwsCR))

		deps := getAllUnmanagedDependencies()
		result := BuildHelmCharts(ctx, cli, deps, testChartsPath)

		// cert-manager has operatorCR=nil (CM-1019), so it goes straight to
		// chartExcluded even with CertManager CR on cluster.
		g.Expect(result.Charts).To(HaveLen(2))
		g.Expect(result.Charts[0].ReleaseName).To(Equal("lws-operator"))
		g.Expect(result.Charts[1].ReleaseName).To(Equal("sail-operator"))
		g.Expect(result.FilterCRs).To(HaveLen(2))
	})

	t.Run("unmanaged with CR gone adds chart to CleanupCharts", func(t *testing.T) {
		g := NewWithT(t)
		cli := newFakeClient(t)

		deps := getAllUnmanagedDependencies()
		result := BuildHelmCharts(ctx, cli, deps, testChartsPath)

		g.Expect(result.Charts).To(BeEmpty())
		g.Expect(result.FilterCRs).To(BeEmpty())
		g.Expect(result.CleanupCharts).To(HaveLen(3))
	})

	t.Run("mixed states across dependencies", func(t *testing.T) {
		g := NewWithT(t)

		istioCR := &unstructured.Unstructured{}
		istioCR.SetGroupVersionKind(gvk.Istio)
		istioCR.SetName("default")
		istioCR.SetNamespace(ccmcommon.DefaultNamespaceSailOperator)

		cli := newFakeClient(t, fakeclient.WithObjects(istioCR))

		deps := ccmcommon.Dependencies{
			GatewayAPI:   ccmcommon.GatewayAPIDependency{ManagementPolicy: ccmcommon.Managed},
			CertManager:  ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
			LWS:          ccmcommon.LWSDependency{ManagementPolicy: ccmcommon.Unmanaged},
			SailOperator: ccmcommon.SailOperatorDependency{ManagementPolicy: ccmcommon.Unmanaged},
		}

		result := BuildHelmCharts(ctx, cli, deps, testChartsPath)

		g.Expect(result.Charts).To(HaveLen(3))
		g.Expect(result.Charts[0].ReleaseName).To(Equal("gateway-api"))
		g.Expect(result.Charts[1].ReleaseName).To(Equal("cert-manager-operator"))
		g.Expect(result.Charts[2].ReleaseName).To(Equal("sail-operator"))
		g.Expect(result.FilterCRs).To(HaveLen(1))
		g.Expect(result.FilterCRs[0].GVK).To(Equal(gvk.Istio))
		g.Expect(result.CleanupCharts).To(HaveLen(1))
		g.Expect(result.CleanupCharts[0].ReleaseName).To(Equal("lws-operator"))
	})
}
