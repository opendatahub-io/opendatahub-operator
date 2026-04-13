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

// chartDef describes a single Helm chart together with its target namespace
// and a function that extracts the chart's management policy from Dependencies.
type chartDef struct {
	policyFn func(ccmcommon.Dependencies) ccmcommon.ManagementPolicy
	chart    types.HelmChartInfo
}

// allChartDefs is the single source of truth for all charts and their target
// namespaces. BuildHelmCharts derive from this list.
// Namespaces are resolved from the Dependencies via the dependency getters.
func allChartDefs(deps ccmcommon.Dependencies) []chartDef {
	return []chartDef{
		{
			policyFn: func(d ccmcommon.Dependencies) ccmcommon.ManagementPolicy {
				return d.GatewayAPI.ManagementPolicy
			},
			chart: types.HelmChartInfo{
				Source: helm.Source{
					Chart:       filepath.Join(DefaultChartsPath, "gateway-api"),
					ReleaseName: "gateway-api",
					Values:      helm.Values(map[string]any{}),
				},
			},
		},
		{
			policyFn: func(d ccmcommon.Dependencies) ccmcommon.ManagementPolicy {
				return d.CertManager.ManagementPolicy
			},
			chart: types.HelmChartInfo{
				Source: helm.Source{
					Chart:       filepath.Join(DefaultChartsPath, "cert-manager-operator"),
					ReleaseName: "cert-manager-operator",
					Values: helm.Values(map[string]any{
						"operatorNamespace": "cert-manager-operator",
						"operandNamespace":  "cert-manager",
					}),
				},
				PreApply: []types.HookFn{},
			},
		},
		{
			policyFn: func(d ccmcommon.Dependencies) ccmcommon.ManagementPolicy {
				return d.LWS.ManagementPolicy
			},
			chart: types.HelmChartInfo{
				Source: helm.Source{
					Chart:       filepath.Join(DefaultChartsPath, "lws-operator"),
					ReleaseName: "lws-operator",
					Values: helm.Values(map[string]any{
						"namespace": deps.LWS.GetNamespace(),
					}),
				},
				PreApply: []types.HookFn{SkipCRDIfPresent(ServiceMonitorCRDName)},
			},
		},
		{
			policyFn: func(d ccmcommon.Dependencies) ccmcommon.ManagementPolicy {
				return d.SailOperator.ManagementPolicy
			},
			chart: types.HelmChartInfo{
				Source: helm.Source{
					Chart:       filepath.Join(DefaultChartsPath, "sail-operator"),
					ReleaseName: "sail-operator",
					Values: helm.Values(map[string]any{
						"namespace": deps.SailOperator.GetNamespace(),
					}),
				},
				PreApply: []types.HookFn{},
				// TODO(OSSM-12397): Remove PostApply hook once the sail-operator ships a fix.
				PostApply: []types.HookFn{AnnotateIstioWebhooksHook()},
			},
		},
	}
}

// BuildHelmCharts returns the charts filtered by management policy,
// in deterministic installation order. Namespaces are resolved from deps.
func BuildHelmCharts(deps ccmcommon.Dependencies) []types.HelmChartInfo {
	var charts []types.HelmChartInfo

	for _, def := range allChartDefs(deps) {
		if def.policyFn(deps) != ccmcommon.Unmanaged {
			charts = append(charts, def.chart)
		}
	}

	return charts
}
