package common

import (
	"os"
	"path/filepath"

	helm "github.com/k8s-manifest-kit/renderer-helm/pkg"

	ccmcommon "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// DefaultChartsPath is the base directory for locally-bundled Helm charts.
// It mirrors the pattern of DefaultManifestPath in pkg/deploy/deploy.go.
var DefaultChartsPath = os.Getenv("DEFAULT_CHARTS_PATH")

// BuildHelmCharts returns the default charts filtered by management policy,
// in deterministic installation order.
func BuildHelmCharts(deps ccmcommon.Dependencies) []types.HelmChartInfo {
	type entry struct {
		policy ccmcommon.ManagementPolicy
		chart  types.HelmChartInfo
	}

	all := []entry{
		{
			policy: deps.CertManager.ManagementPolicy,
			chart: types.HelmChartInfo{
				Source: helm.Source{
					Chart:       filepath.Join(DefaultChartsPath, "cert-manager-operator"),
					ReleaseName: "cert-manager-operator",
					Values:      helm.Values(map[string]any{}),
				},
				PreApply: []types.HookFn{
					CreateNamespaceHook("cert-manager-operator"),
					CreateNamespaceHook("cert-manager"),
				},
			},
		},
		{
			policy: deps.LWS.ManagementPolicy,
			chart: types.HelmChartInfo{
				Source: helm.Source{
					Chart:       filepath.Join(DefaultChartsPath, "lws-operator"),
					ReleaseName: "lws-operator",
					Values:      helm.Values(map[string]any{}),
				},
				PreApply: []types.HookFn{
					CreateNamespaceHook("openshift-lws-operator"),
				},
			},
		},
		{
			policy: deps.SailOperator.ManagementPolicy,
			chart: types.HelmChartInfo{
				Source: helm.Source{
					Chart:       filepath.Join(DefaultChartsPath, "sail-operator"),
					ReleaseName: "sail-operator",
					Values:      helm.Values(map[string]any{}),
				},
				PreApply: []types.HookFn{
					CreateNamespaceHook("istio-system"),
				},
			},
		},
	}

	var charts []types.HelmChartInfo
	for _, e := range all {
		if e.policy != ccmcommon.Unmanaged {
			charts = append(charts, e.chart)
		}
	}

	return charts
}
