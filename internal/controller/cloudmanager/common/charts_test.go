//nolint:testpackage // testing unexported methods
package common

import (
	"path/filepath"
	"testing"

	ccmcommon "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"

	. "github.com/onsi/gomega"
)

func TestManagedNamespaces(t *testing.T) {
	t.Run("returns namespaces derived from chart registry", func(t *testing.T) {
		g := NewWithT(t)

		namespaces := ManagedNamespaces()

		g.Expect(namespaces).To(HaveLen(3))
		g.Expect(namespaces).To(Equal([]string{
			NamespaceCertManagerOperator,
			NamespaceLWSOperator,
			NamespaceSailOperator,
		}))
	})
}

func TestBuildHelmCharts(t *testing.T) {
	t.Run("returns all charts in order when all managed", func(t *testing.T) {
		g := NewWithT(t)

		deps := ccmcommon.Dependencies{
			CertManager:  ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
			LWS:          ccmcommon.LWSDependency{ManagementPolicy: ccmcommon.Managed},
			SailOperator: ccmcommon.SailOperatorDependency{ManagementPolicy: ccmcommon.Managed},
		}

		charts := BuildHelmCharts(deps)

		g.Expect(charts).To(HaveLen(3))
		g.Expect(charts[0].ReleaseName).To(Equal("cert-manager-operator"))
		g.Expect(charts[1].ReleaseName).To(Equal("lws-operator"))
		g.Expect(charts[2].ReleaseName).To(Equal("sail-operator"))
	})

	t.Run("excludes unmanaged charts and preserves order", func(t *testing.T) {
		g := NewWithT(t)

		deps := ccmcommon.Dependencies{
			CertManager:  ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Unmanaged},
			LWS:          ccmcommon.LWSDependency{ManagementPolicy: ccmcommon.Managed},
			SailOperator: ccmcommon.SailOperatorDependency{ManagementPolicy: ccmcommon.Unmanaged},
		}

		charts := BuildHelmCharts(deps)

		g.Expect(charts).To(HaveLen(1))
		g.Expect(charts[0].ReleaseName).To(Equal("lws-operator"))
	})

	t.Run("returns empty slice when all unmanaged", func(t *testing.T) {
		g := NewWithT(t)

		deps := ccmcommon.Dependencies{
			CertManager:  ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Unmanaged},
			LWS:          ccmcommon.LWSDependency{ManagementPolicy: ccmcommon.Unmanaged},
			SailOperator: ccmcommon.SailOperatorDependency{ManagementPolicy: ccmcommon.Unmanaged},
		}

		charts := BuildHelmCharts(deps)

		g.Expect(charts).To(BeEmpty())
	})

	t.Run("keeps charts when ManagementPolicy is zero value", func(t *testing.T) {
		g := NewWithT(t)

		deps := ccmcommon.Dependencies{} // All zero values

		charts := BuildHelmCharts(deps)

		// Zero-value ManagementPolicy ("") is not "Unmanaged", so charts remain.
		// In practice, kubebuilder defaults ManagementPolicy to "Managed".
		g.Expect(charts).To(HaveLen(3))
	})

	t.Run("chart paths use DefaultChartsPath", func(t *testing.T) {
		g := NewWithT(t)

		original := DefaultChartsPath
		DefaultChartsPath = "/test/charts"
		t.Cleanup(func() { DefaultChartsPath = original })

		deps := ccmcommon.Dependencies{
			CertManager:  ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
			LWS:          ccmcommon.LWSDependency{ManagementPolicy: ccmcommon.Managed},
			SailOperator: ccmcommon.SailOperatorDependency{ManagementPolicy: ccmcommon.Managed},
		}

		charts := BuildHelmCharts(deps)

		g.Expect(charts).To(HaveLen(3))
		g.Expect(charts[0].Chart).To(Equal(filepath.Join("/test/charts", "cert-manager-operator")))
		g.Expect(charts[1].Chart).To(Equal(filepath.Join("/test/charts", "lws-operator")))
		g.Expect(charts[2].Chart).To(Equal(filepath.Join("/test/charts", "sail-operator")))
	})
}
