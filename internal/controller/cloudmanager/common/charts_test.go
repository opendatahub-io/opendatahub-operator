//nolint:testpackage // testing unexported methods
package common

import (
	"path/filepath"
	"testing"

	ccmcommon "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"

	. "github.com/onsi/gomega"
)

const testChartsPath = "/test/charts"

func TestManagedNamespaces(t *testing.T) {
	t.Run("returns namespaces derived from chart registry", func(t *testing.T) {
		g := NewWithT(t)

		namespaces := ManagedNamespaces(testChartsPath)

		g.Expect(namespaces).To(HaveLen(3))
		g.Expect(namespaces).To(Equal([]string{
			NamespaceCertManagerOperator,
			NamespaceLWSOperator,
			NamespaceSailOperator,
		}))
	})
}

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

		// Zero-value ManagementPolicy ("") is not "Unmanaged", so all charts are returned.
		// In practice, kubebuilder defaults ManagementPolicy to "Managed".
		charts := BuildHelmCharts(testChartsPath, ccmcommon.Dependencies{})

		g.Expect(charts).To(HaveLen(len(expectedReleaseNames)))
		for i, name := range expectedReleaseNames {
			g.Expect(charts[i].ReleaseName).To(Equal(name))
			g.Expect(charts[i].Chart).To(Equal(filepath.Join(testChartsPath, name)))
		}
	})

	t.Run("excludes unmanaged charts and preserves order", func(t *testing.T) {
		g := NewWithT(t)

		deps := getAllUnmanagedDependencies()
		deps.LWS.ManagementPolicy = ccmcommon.Managed

		charts := BuildHelmCharts(testChartsPath, deps)

		g.Expect(charts).To(HaveLen(1))
		g.Expect(charts[0].ReleaseName).To(Equal("lws-operator"))
	})

	t.Run("returns empty slice when all unmanaged", func(t *testing.T) {
		g := NewWithT(t)

		deps := getAllUnmanagedDependencies()

		charts := BuildHelmCharts(testChartsPath, deps)

		g.Expect(charts).To(BeEmpty())
	})
}
