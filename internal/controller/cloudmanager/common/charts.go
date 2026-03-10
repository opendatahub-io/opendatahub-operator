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

const (
	NamespaceCertManagerOperator = "cert-manager-operator"
	NamespaceLWSOperator         = "openshift-lws-operator"
	NamespaceSailOperator        = "istio-system"
)

// chartDef describes a single Helm chart together with its target namespace
// and a function that extracts the chart's management policy from Dependencies.
type chartDef struct {
	namespace string
	policyFn  func(ccmcommon.Dependencies) ccmcommon.ManagementPolicy
	chart     types.HelmChartInfo
}

// allChartDefs is the single source of truth for all charts and their target
// namespaces. Both ManagedNamespaces and BuildHelmCharts derive from this list.
func allChartDefs() []chartDef {
	return []chartDef{
		{
			namespace: NamespaceCertManagerOperator,
			policyFn: func(d ccmcommon.Dependencies) ccmcommon.ManagementPolicy {
				return d.CertManager.ManagementPolicy
			},
			chart: types.HelmChartInfo{
				Source: helm.Source{
					Chart:       filepath.Join(DefaultChartsPath, "cert-manager-operator"),
					ReleaseName: "cert-manager-operator",
					Values: helm.Values(map[string]any{
						"operatorNamespace": NamespaceCertManagerOperator,
					}),
				},
				PreApply: []types.HookFn{},
			},
		},
		{
			namespace: NamespaceLWSOperator,
			policyFn: func(d ccmcommon.Dependencies) ccmcommon.ManagementPolicy {
				return d.LWS.ManagementPolicy
			},
			chart: types.HelmChartInfo{
				Source: helm.Source{
					Chart:       filepath.Join(DefaultChartsPath, "lws-operator"),
					ReleaseName: "lws-operator",
					Values: helm.Values(map[string]any{
						"namespace": NamespaceLWSOperator,
					}),
				},
				PreApply: []types.HookFn{},
			},
		},
		{
			namespace: NamespaceSailOperator,
			policyFn: func(d ccmcommon.Dependencies) ccmcommon.ManagementPolicy {
				return d.SailOperator.ManagementPolicy
			},
			chart: types.HelmChartInfo{
				Source: helm.Source{
					Chart:       filepath.Join(DefaultChartsPath, "sail-operator"),
					ReleaseName: "sail-operator",
					Values: helm.Values(map[string]any{
						"namespace": NamespaceSailOperator,
					}),
				},
				PreApply: []types.HookFn{},
			},
		},
	}
}

// ManagedNamespaces returns all namespaces the cache must watch,
// derived from the central chart registry.
func ManagedNamespaces() []string {
	seen := make(map[string]struct{})
	var namespaces []string

	for _, def := range allChartDefs() {
		if _, ok := seen[def.namespace]; !ok {
			seen[def.namespace] = struct{}{}
			namespaces = append(namespaces, def.namespace)
		}
	}

	return namespaces
}

// BuildHelmCharts returns the default charts filtered by management policy,
// in deterministic installation order.
func BuildHelmCharts(deps ccmcommon.Dependencies) []types.HelmChartInfo {
	var charts []types.HelmChartInfo

	for _, def := range allChartDefs() {
		if def.policyFn(deps) != ccmcommon.Unmanaged {
			charts = append(charts, def.chart)
		}
	}

	return charts
}
