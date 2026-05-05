package common

import (
	"path/filepath"

	helm "github.com/k8s-manifest-kit/renderer-helm/pkg"

	ccmcommon "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// chartDef describes a single Helm chart together with its target namespace
// and a function that extracts the chart's management policy from Dependencies.
type chartDef struct {
	policyFn func(ccmcommon.Dependencies) ccmcommon.ManagementPolicy
	chart    types.HelmChartInfo
}

// allChartDefs is the single source of truth for all charts and their target
// namespaces. BuildHelmCharts derives from this list.
func allChartDefs(deps ccmcommon.Dependencies, chartsPath string) []chartDef {
	return []chartDef{
		{
			policyFn: func(d ccmcommon.Dependencies) ccmcommon.ManagementPolicy {
				return d.GatewayAPI.ManagementPolicy
			},
			chart: types.HelmChartInfo{
				Source: helm.Source{
					Chart:       filepath.Join(chartsPath, "gateway-api"),
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
					Chart:       filepath.Join(chartsPath, "cert-manager-operator"),
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
					Chart:       filepath.Join(chartsPath, "lws-operator"),
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
					Chart:       filepath.Join(chartsPath, "sail-operator"),
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
func BuildHelmCharts(deps ccmcommon.Dependencies, chartsPath string) []types.HelmChartInfo {
	var charts []types.HelmChartInfo

	for _, def := range allChartDefs(deps, chartsPath) {
		if def.policyFn(deps) != ccmcommon.Unmanaged {
			charts = append(charts, def.chart)
		}
	}

	return charts
}
