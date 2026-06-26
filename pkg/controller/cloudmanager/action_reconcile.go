package cloudmanager

import (
	"context"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	ccmcommon "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"
	ccmcharts "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/helm"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

var ConditionsTypes = []string{
	status.ConditionDependenciesReady,
}

// reconcileAction holds the configuration for the combined reconcile action.
type reconcileAction struct {
	helmOpts      []helm.ActionOpts
	deployOpts    []deploy.ActionOpts
	resourceID    string
	buildChartsFn func(context.Context, *types.ReconciliationRequest) (ccmcharts.BuildResult, error)
}

// ReconcileActionOpts configures the combined reconcile action.
type ReconcileActionOpts func(*reconcileAction)

// WithHelmOptions adds Helm rendering options.
func WithHelmOptions(opts ...helm.ActionOpts) ReconcileActionOpts {
	return func(a *reconcileAction) {
		a.helmOpts = append(a.helmOpts, opts...)
	}
}

// WithDeployOptions adds deploy action options.
func WithDeployOptions(opts ...deploy.ActionOpts) ReconcileActionOpts {
	return func(a *reconcileAction) {
		a.deployOpts = append(a.deployOpts, opts...)
	}
}

// WithBuildChartsFn replaces the default buildCharts implementation.
// Useful for testing or for callers that need custom chart discovery logic.
func WithBuildChartsFn(fn func(context.Context, *types.ReconciliationRequest) (ccmcharts.BuildResult, error)) ReconcileActionOpts {
	return func(a *reconcileAction) {
		a.buildChartsFn = fn
	}
}

func buildCharts(ctx context.Context, rr *types.ReconciliationRequest) (ccmcharts.BuildResult, error) {
	if rr.ChartsBasePath == "" {
		return ccmcharts.BuildResult{}, errors.New("ChartsBasePath must not be empty")
	}

	dp, ok := rr.Instance.(ccmcommon.KubernetesEngineInstance)
	if !ok {
		return ccmcharts.BuildResult{}, fmt.Errorf("instance %T does not implement KubernetesEngineInstance", rr.Instance)
	}

	return ccmcharts.BuildHelmCharts(ctx, rr.Client, dp.GetDependencies(), rr.ChartsBasePath)
}

func filterCRs(resources []unstructured.Unstructured, crs []types.OperatorCR) []unstructured.Unstructured {
	if len(crs) == 0 {
		return resources
	}

	type namespacedKey struct {
		gvk       schema.GroupVersionKind
		namespace string
		name      string
	}
	// clusterScopedKey is used for cluster-scoped CRs (Namespace=="") whose Helm
	// templates may set metadata.namespace (which the API server ignores for cluster-scoped
	// resources but the renderer preserves in the unstructured object). Matching by
	// GVK+Name alone ensures filtering works regardless of what the renderer emits.
	type clusterScopedKey struct {
		gvk  schema.GroupVersionKind
		name string
	}

	exact := make(map[namespacedKey]struct{}, len(crs))
	clusterScoped := make(map[clusterScopedKey]struct{}, len(crs))

	for _, cr := range crs {
		if cr.Namespace == "" {
			clusterScoped[clusterScopedKey{cr.GVK, cr.Name}] = struct{}{}
		} else {
			exact[namespacedKey{cr.GVK, cr.Namespace, cr.Name}] = struct{}{}
		}
	}

	filtered := make([]unstructured.Unstructured, 0, len(resources))

	for _, res := range resources {
		resGVK := res.GroupVersionKind()
		_, exactMatch := exact[namespacedKey{resGVK, res.GetNamespace(), res.GetName()}]
		_, clusterMatch := clusterScoped[clusterScopedKey{resGVK, res.GetName()}]

		if !exactMatch && !clusterMatch {
			filtered = append(filtered, res)
		}
	}

	return filtered
}

// NewReconcileAction creates a combined action that:
// - Builds the chart list (with two-phase cleanup for Unmanaged dependencies)
// - Renders Helm charts
// - Filters operator CRs from Phase 1 cleanup charts
// - Runs PreApply hooks from HelmCharts
// - Deploys resources via SSA
// - Runs PostApply hooks from HelmCharts
// - Checks per-dependency health (deployment readiness + operator CR health).
//
// Per-dependency monitoring is enabled automatically when rr.Instance implements
// ccmcommon.KubernetesEngineInstance.
func NewReconcileAction(resourceID string, opts ...ReconcileActionOpts) (actions.Fn, error) {
	resourceID = labels.NormalizePartOfValue(resourceID)
	if resourceID == "" {
		return nil, errors.New("resourceID is required")
	}

	action := reconcileAction{
		resourceID:    resourceID,
		buildChartsFn: buildCharts,
	}

	for _, opt := range opts {
		opt(&action)
	}

	helmRender := helm.NewAction(action.helmOpts...)
	deployAction := deploy.NewAction(append(action.deployOpts,
		deploy.WithApplyOrder(),
		deploy.WithContinueOnError(),
		deploy.WithPartOfLabel(labels.InfrastructurePartOf),
		deploy.WithAnnotationPrefix(labels.ODHInfrastructurePrefix),
	)...)

	return func(ctx context.Context, rr *types.ReconciliationRequest) error {
		result, err := action.buildChartsFn(ctx, rr)
		if err != nil {
			return err
		}

		rr.HelmCharts = append(rr.HelmCharts, result.Charts...)

		if err := helmRender(ctx, rr); err != nil {
			return fmt.Errorf("helm render failed: %w", err)
		}

		rr.Resources = filterCRs(rr.Resources, result.FilterCRs)

		// Execute PreApply hooks
		err = runHooks(ctx, rr, func(c *types.HelmChartInfo) []types.HookFn {
			return c.PreApply
		})
		if err != nil {
			return err
		}

		// Deploy resources via SSA
		if err := deployAction(ctx, rr); err != nil {
			return fmt.Errorf("deploy failed: %w", err)
		}

		// Execute PostApply hooks
		err = runHooks(ctx, rr, func(c *types.HelmChartInfo) []types.HookFn {
			return c.PostApply
		})
		if err != nil {
			return err
		}

		if err := cleanupExcludedCharts(ctx, rr, result.CleanupCharts); err != nil {
			return err
		}

		if err := monitorDependencies(ctx, rr, action.resourceID, result.MonitorConfigs); err != nil {
			return err
		}

		summarizeDependencyStatus(rr, result.MonitorConfigs)

		return nil
	}, nil
}
