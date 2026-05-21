package common

import (
	"path/filepath"

	helm "github.com/k8s-manifest-kit/renderer-helm/pkg"
	"k8s.io/apimachinery/pkg/runtime/schema"

	ccmcommon "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// chartDef describes a single Helm chart together with its target namespace
// and a function that extracts the chart's management policy from Dependencies.
type chartDef struct {
	policyFn func(ccmcommon.Dependencies) ccmcommon.ManagementPolicy
	chart    types.HelmChartInfo
	monitor  monitorConfig
}

// monitorConfig holds per-dependency monitoring metadata embedded in chartDef.
type monitorConfig struct {
	ConditionType  string
	HasDeployments bool
	Namespace      string
	OperatorGVK    schema.GroupVersionKind
	CRName         string
	CRNamespace    string
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
			monitor: monitorConfig{
				ConditionType:  status.ConditionGatewayAPIReady,
				HasDeployments: false,
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
						"operatorNamespace": ccmcommon.DefaultNamespaceCertManagerOperator,
						"operandNamespace":  ccmcommon.DefaultNamespaceCertManagerOperand,
					}),
				},
				PreApply: []types.HookFn{},
			},
			monitor: monitorConfig{
				ConditionType:  status.ConditionCertManagerReady,
				HasDeployments: true,
				Namespace:      ccmcommon.DefaultNamespaceCertManagerOperator,
				OperatorGVK:    gvk.CertManagerV1Alpha1,
				CRName:         "cluster",
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
			monitor: monitorConfig{
				ConditionType:  status.ConditionLWSReady,
				HasDeployments: true,
				Namespace:      deps.LWS.GetNamespace(),
				OperatorGVK:    gvk.LeaderWorkerSetOperatorV1,
				CRName:         "cluster",
				CRNamespace:    deps.LWS.GetNamespace(),
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
			monitor: monitorConfig{
				ConditionType:  status.ConditionSailOperatorReady,
				HasDeployments: true,
				Namespace:      deps.SailOperator.GetNamespace(),
				OperatorGVK:    gvk.Istio,
				CRName:         "default",
				CRNamespace:    deps.SailOperator.GetNamespace(),
			},
		},
	}
}

// DependencyMonitorConfig holds per-dependency monitoring metadata.
type DependencyMonitorConfig struct {
	ReleaseName    string
	ConditionType  string
	HasDeployments bool
	Policy         ccmcommon.ManagementPolicy
	Namespace      string
	OperatorGVK    schema.GroupVersionKind
	CRName         string
	CRNamespace    string
}

// AllDependencyMonitorConfigs returns monitoring configs for all 4 dependencies,
// derived from the same chartDef source of truth used by BuildHelmCharts.
func AllDependencyMonitorConfigs(deps ccmcommon.Dependencies, chartsPath string) []DependencyMonitorConfig {
	defs := allChartDefs(deps, chartsPath)
	configs := make([]DependencyMonitorConfig, 0, len(defs))

	for _, def := range defs {
		configs = append(configs, DependencyMonitorConfig{
			ReleaseName:    def.chart.ReleaseName,
			ConditionType:  def.monitor.ConditionType,
			HasDeployments: def.monitor.HasDeployments,
			Policy:         def.policyFn(deps),
			Namespace:      def.monitor.Namespace,
			OperatorGVK:    def.monitor.OperatorGVK,
			CRName:         def.monitor.CRName,
			CRNamespace:    def.monitor.CRNamespace,
		})
	}

	return configs
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
