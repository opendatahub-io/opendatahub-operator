//nolint:testpackage // testing unexported methods
package common

import (
	"path/filepath"
	"testing"

	ccmcommon "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"

	. "github.com/onsi/gomega"
)

func getAllUnmanagedDependencies() ccmcommon.Dependencies {
	return ccmcommon.Dependencies{
		GatewayAPI:   ccmcommon.GatewayAPIDependency{ManagementPolicy: ccmcommon.Unmanaged},
		CertManager:  ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Unmanaged},
		LWS:          ccmcommon.LWSDependency{ManagementPolicy: ccmcommon.Unmanaged},
		SailOperator: ccmcommon.SailOperatorDependency{ManagementPolicy: ccmcommon.Unmanaged},
	}
}

func TestBuildHelmCharts(t *testing.T) {
	// expectedReleaseNames is the hardcoded ground truth for chart names and order.
	// Update this list when adding or removing a chart.
	expectedReleaseNames := []string{
		"gateway-api",
		"cert-manager-operator",
		"lws-operator",
		"sail-operator",
	}

	t.Run("returns all charts in order when all managed", func(t *testing.T) {
		g := NewWithT(t)

		original := DefaultChartsPath
		DefaultChartsPath = "/test/charts"
		t.Cleanup(func() { DefaultChartsPath = original })

		// Zero-value ManagementPolicy ("") is not "Unmanaged", so all charts are returned.
		// In practice, kubebuilder defaults ManagementPolicy to "Managed".
		charts := BuildHelmCharts(ccmcommon.Dependencies{})

		g.Expect(charts).To(HaveLen(len(expectedReleaseNames)))
		for i, name := range expectedReleaseNames {
			g.Expect(charts[i].ReleaseName).To(Equal(name))
			g.Expect(charts[i].Chart).To(Equal(filepath.Join("/test/charts", name)))
		}
	})

	t.Run("excludes unmanaged charts and preserves order", func(t *testing.T) {
		g := NewWithT(t)

		deps := getAllUnmanagedDependencies()
		deps.LWS.ManagementPolicy = ccmcommon.Managed

		charts := BuildHelmCharts(deps)

		g.Expect(charts).To(HaveLen(1))
		g.Expect(charts[0].ReleaseName).To(Equal("lws-operator"))
	})

	t.Run("returns empty slice when all unmanaged", func(t *testing.T) {
		g := NewWithT(t)

		deps := getAllUnmanagedDependencies()

		charts := BuildHelmCharts(deps)

		g.Expect(charts).To(BeEmpty())
	})

	t.Run("uses custom namespaces in chart values", func(t *testing.T) {
		g := NewWithT(t)

		original := DefaultChartsPath
		DefaultChartsPath = "/test/charts"
		t.Cleanup(func() { DefaultChartsPath = original })

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

		charts := BuildHelmCharts(deps)

		g.Expect(charts).To(HaveLen(4))

		// cert-manager-operator chart should have hardcoded namespace in values
		certManagerChart := charts[1]
		g.Expect(certManagerChart.ReleaseName).To(Equal("cert-manager-operator"))
		values, err := certManagerChart.Values(nil)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(values).To(HaveKeyWithValue("operatorNamespace", "cert-manager-operator"))
		g.Expect(values).To(HaveKeyWithValue("operandNamespace", "cert-manager"))

		// lws-operator chart should have custom namespace in values
		lwsChart := charts[2]
		g.Expect(lwsChart.ReleaseName).To(Equal("lws-operator"))
		values, err = lwsChart.Values(nil)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(values).To(HaveKeyWithValue("namespace", "custom-lws-ns"))

		// sail-operator chart should have custom namespace in values
		sailChart := charts[3]
		g.Expect(sailChart.ReleaseName).To(Equal("sail-operator"))
		values, err = sailChart.Values(nil)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(values).To(HaveKeyWithValue("namespace", "custom-sail-ns"))
	})
}
