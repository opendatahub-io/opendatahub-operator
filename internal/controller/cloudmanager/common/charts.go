package common

import (
	"context"
	"path/filepath"

	helm "github.com/k8s-manifest-kit/renderer-helm/pkg"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ccmcommon "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

type chartState int

const (
	chartManaged  chartState = iota // render + deploy all resources
	chartCleaning                   // Phase 1: render + deploy operator resources, filter CR
	chartExcluded                   // Phase 2: skip entirely
)

// chartDef describes a single Helm chart together with its monitoring
// metadata and a function that computes its state based on management
// policy and cluster state.
type chartDef struct {
	stateFn    func(ctx context.Context, cli client.Client, d ccmcommon.Dependencies) chartState
	chart      types.HelmChartInfo
	monitor    monitorConfig
	operatorCR *types.OperatorCR
}

// monitorConfig holds per-dependency monitoring metadata embedded in chartDef.
// Operator CR info (GVK, name, namespace) comes from chart.OperatorCR.
type monitorConfig struct {
	ConditionType  string
	HasDeployments bool
	Namespace      string
}

// makeStateFn returns a stateFn that checks the management policy and,
// for Unmanaged dependencies with an OperatorCR, keeps the chart while
// the CR exists on cluster (Phase 1).
func makeStateFn(
	policyFn func(ccmcommon.Dependencies) ccmcommon.ManagementPolicy,
	operatorCR *types.OperatorCR,
) func(ctx context.Context, cli client.Client, d ccmcommon.Dependencies) chartState {
	return func(ctx context.Context, cli client.Client, d ccmcommon.Dependencies) chartState {
		if policyFn(d) != ccmcommon.Unmanaged {
			return chartManaged
		}

		if operatorCR != nil && operatorCRExists(ctx, cli, operatorCR) {
			return chartCleaning
		}

		return chartExcluded
	}
}

// allChartDefs is the single source of truth for all charts and their target
// namespaces. BuildHelmCharts derives from this list.
func allChartDefs(deps ccmcommon.Dependencies, chartsPath string) []chartDef {
	certManagerOperatorCR := &types.OperatorCR{
		GVK:  gvk.CertManagerV1Alpha1,
		Name: "cluster",
	}

	lwsOperatorCR := &types.OperatorCR{
		GVK:       gvk.LeaderWorkerSetOperatorV1,
		Name:      "cluster",
		Namespace: deps.LWS.GetNamespace(),
	}

	sailOperatorCR := &types.OperatorCR{
		GVK:       gvk.Istio,
		Name:      "default",
		Namespace: deps.SailOperator.GetNamespace(),
	}

	return []chartDef{
		{
			stateFn: makeStateFn(func(d ccmcommon.Dependencies) ccmcommon.ManagementPolicy {
				return d.GatewayAPI.ManagementPolicy
			}, nil),
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
			// FIXME(CM-1019): cert-manager-operator recreates CertManager/cluster after
			// deletion, blocking Phase 1→Phase 2 cleanup. operatorCR is set to nil to
			// skip the two-phase mechanism; cleanup relies on GC via generation mismatch.
			stateFn: makeStateFn(func(d ccmcommon.Dependencies) ccmcommon.ManagementPolicy {
				return d.CertManager.ManagementPolicy
			}, nil),
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
			operatorCR: certManagerOperatorCR,
			monitor: monitorConfig{
				ConditionType:  status.ConditionCertManagerReady,
				HasDeployments: true,
				Namespace:      ccmcommon.DefaultNamespaceCertManagerOperator,
			},
		},
		{
			stateFn: makeStateFn(func(d ccmcommon.Dependencies) ccmcommon.ManagementPolicy {
				return d.LWS.ManagementPolicy
			}, lwsOperatorCR),
			operatorCR: lwsOperatorCR,
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
			},
		},
		{
			stateFn: makeStateFn(func(d ccmcommon.Dependencies) ccmcommon.ManagementPolicy {
				return d.SailOperator.ManagementPolicy
			}, sailOperatorCR),
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
			operatorCR: sailOperatorCR,
			monitor: monitorConfig{
				ConditionType:  status.ConditionSailOperatorReady,
				HasDeployments: true,
				Namespace:      deps.SailOperator.GetNamespace(),
			},
		},
	}
}

// DependencyMonitorConfig holds per-dependency monitoring metadata.
// Operator CR info is derived from chartDef.operatorCR.
type DependencyMonitorConfig struct {
	ReleaseName    string
	ConditionType  string
	HasDeployments bool
	Policy         ccmcommon.ManagementPolicy
	Namespace      string
	OperatorCR     *types.OperatorCR
}

// BuildResult holds the output of BuildHelmCharts: the charts to render,
// any operator CRs to filter from deploy (Phase 1 cleanup), charts whose
// resources should be deleted (Phase 2 cleanup), and monitoring configs
// for all dependencies.
type BuildResult struct {
	Charts         []types.HelmChartInfo
	FilterCRs      []types.OperatorCR
	CleanupCharts  []types.HelmChartInfo
	MonitorConfigs []DependencyMonitorConfig
}

// BuildHelmCharts returns the charts to render, CRs to filter, and monitoring
// configs in a single pass. Each chart's stateFn is called exactly once.
func BuildHelmCharts(ctx context.Context, cli client.Client, deps ccmcommon.Dependencies, chartsPath string) BuildResult {
	var result BuildResult

	for _, def := range allChartDefs(deps, chartsPath) {
		state := def.stateFn(ctx, cli, deps)

		policy := ccmcommon.Unmanaged

		switch state {
		case chartManaged:
			policy = ccmcommon.Managed
			result.Charts = append(result.Charts, def.chart)
		case chartCleaning:
			result.Charts = append(result.Charts, def.chart)
			if def.operatorCR != nil {
				result.FilterCRs = append(result.FilterCRs, *def.operatorCR)
			}
		case chartExcluded:
			if def.operatorCR != nil {
				result.CleanupCharts = append(result.CleanupCharts, def.chart)
			}
		}

		result.MonitorConfigs = append(result.MonitorConfigs, DependencyMonitorConfig{
			ReleaseName:    def.chart.ReleaseName,
			ConditionType:  def.monitor.ConditionType,
			HasDeployments: def.monitor.HasDeployments,
			Policy:         policy,
			Namespace:      def.monitor.Namespace,
			OperatorCR:     def.operatorCR,
		})
	}

	return result
}

func operatorCRExists(ctx context.Context, cli client.Client, cr *types.OperatorCR) bool {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(cr.GVK)

	err := cli.Get(ctx, client.ObjectKey{Name: cr.Name, Namespace: cr.Namespace}, obj)
	if err != nil {
		if k8serr.IsNotFound(err) || meta.IsNoMatchError(err) {
			return false
		}

		return false
	}

	return true
}
