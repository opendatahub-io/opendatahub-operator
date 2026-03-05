//nolint:testpackage // testing unexported methods
package common

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ccmcommon "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"
)

func TestBuildHelmCharts(t *testing.T) {
	t.Run("returns all charts in order when all managed", func(t *testing.T) {
		deps := ccmcommon.Dependencies{
			CertManager:  ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
			LWS:          ccmcommon.LWSDependency{ManagementPolicy: ccmcommon.Managed},
			SailOperator: ccmcommon.SailOperatorDependency{ManagementPolicy: ccmcommon.Managed},
		}

		charts := BuildHelmCharts(deps)

		require.Len(t, charts, 3)
		assert.Equal(t, "cert-manager-operator", charts[0].ReleaseName)
		assert.Equal(t, "lws-operator", charts[1].ReleaseName)
		assert.Equal(t, "sail-operator", charts[2].ReleaseName)

		assert.Len(t, charts[0].PreApply, 2, "cert-manager-operator should have 2 PreApply hooks")
		assert.Len(t, charts[1].PreApply, 1, "lws-operator should have 1 PreApply hook")
		assert.Len(t, charts[2].PreApply, 1, "sail-operator should have 1 PreApply hook")
	})

	t.Run("excludes unmanaged charts and preserves order", func(t *testing.T) {
		deps := ccmcommon.Dependencies{
			CertManager:  ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Unmanaged},
			LWS:          ccmcommon.LWSDependency{ManagementPolicy: ccmcommon.Managed},
			SailOperator: ccmcommon.SailOperatorDependency{ManagementPolicy: ccmcommon.Unmanaged},
		}

		charts := BuildHelmCharts(deps)

		require.Len(t, charts, 1)
		assert.Equal(t, "lws-operator", charts[0].ReleaseName)
		assert.Len(t, charts[0].PreApply, 1, "lws-operator should have 1 PreApply hook")
	})

	t.Run("returns empty slice when all unmanaged", func(t *testing.T) {
		deps := ccmcommon.Dependencies{
			CertManager:  ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Unmanaged},
			LWS:          ccmcommon.LWSDependency{ManagementPolicy: ccmcommon.Unmanaged},
			SailOperator: ccmcommon.SailOperatorDependency{ManagementPolicy: ccmcommon.Unmanaged},
		}

		charts := BuildHelmCharts(deps)

		assert.Empty(t, charts)
	})

	t.Run("keeps charts when ManagementPolicy is zero value", func(t *testing.T) {
		deps := ccmcommon.Dependencies{} // All zero values

		charts := BuildHelmCharts(deps)

		// Zero-value ManagementPolicy ("") is not "Unmanaged", so charts remain.
		// In practice, kubebuilder defaults ManagementPolicy to "Managed".
		require.Len(t, charts, 3)
	})

	t.Run("chart paths use DefaultChartsPath", func(t *testing.T) {
		original := DefaultChartsPath
		DefaultChartsPath = "/test/charts"
		t.Cleanup(func() { DefaultChartsPath = original })

		deps := ccmcommon.Dependencies{
			CertManager:  ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
			LWS:          ccmcommon.LWSDependency{ManagementPolicy: ccmcommon.Managed},
			SailOperator: ccmcommon.SailOperatorDependency{ManagementPolicy: ccmcommon.Managed},
		}

		charts := BuildHelmCharts(deps)

		require.Len(t, charts, 3)
		assert.Equal(t, filepath.Join("/test/charts", "cert-manager-operator"), charts[0].Chart)
		assert.Equal(t, filepath.Join("/test/charts", "lws-operator"), charts[1].Chart)
		assert.Equal(t, filepath.Join("/test/charts", "sail-operator"), charts[2].Chart)
	})
}
