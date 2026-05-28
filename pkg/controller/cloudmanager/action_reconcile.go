package cloudmanager

import (
	"context"
	"errors"
	"fmt"

	helmRenderer "github.com/k8s-manifest-kit/renderer-helm/pkg"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	ccmcommon "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"
	ccmcharts "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/helm"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

var ConditionsTypes = []string{
	status.ConditionDependenciesReady,
}

// reconcileAction holds the configuration for the combined reconcile action.
type reconcileAction struct {
	helmOpts   []helm.ActionOpts
	deployOpts []deploy.ActionOpts
	resourceID string
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

func buildCharts(ctx context.Context, rr *types.ReconciliationRequest) (ccmcharts.BuildResult, error) {
	dp, ok := rr.Instance.(ccmcommon.KubernetesEngineInstance)
	if !ok {
		return ccmcharts.BuildResult{}, fmt.Errorf("instance %T does not implement KubernetesEngineInstance", rr.Instance)
	}

	return ccmcharts.BuildHelmCharts(ctx, rr.Client, dp.GetDependencies(), rr.ChartsBasePath), nil
}

func filterCRs(resources []unstructured.Unstructured, crs []types.OperatorCR) []unstructured.Unstructured {
	if len(crs) == 0 {
		return resources
	}

	type key struct {
		group, version, kind, name string
	}

	exclude := make(map[key]struct{}, len(crs))
	for _, cr := range crs {
		exclude[key{cr.GVK.Group, cr.GVK.Version, cr.GVK.Kind, cr.Name}] = struct{}{}
	}

	filtered := make([]unstructured.Unstructured, 0, len(resources))

	for _, res := range resources {
		gvk := res.GroupVersionKind()
		if _, ok := exclude[key{gvk.Group, gvk.Version, gvk.Kind, res.GetName()}]; !ok {
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
		resourceID: resourceID,
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
		result, err := buildCharts(ctx, rr)
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

func cleanupExcludedCharts(ctx context.Context, rr *types.ReconciliationRequest, charts []types.HelmChartInfo) error {
	if len(charts) == 0 || !rr.Generated {
		return nil
	}

	l := logf.FromContext(ctx)

	sources := make([]helmRenderer.Source, 0, len(charts))
	for _, c := range charts {
		sources = append(sources, c.Source)
	}

	renderer, err := helmRenderer.New(sources, helmRenderer.RendererOptions{Strict: true})
	if err != nil {
		return fmt.Errorf("cleanup chart render failed: %w", err)
	}

	resources, err := renderer.Process(ctx, map[string]any{})
	if err != nil {
		return fmt.Errorf("cleanup chart render failed: %w", err)
	}

	unremovables := make(map[schema.GroupVersionKind]struct{}, len(gc.DefaultUnremovables)+1)
	for _, u := range gc.DefaultUnremovables {
		unremovables[u] = struct{}{}
	}
	unremovables[gvk.Namespace] = struct{}{}

	for i := range resources {
		obj := &resources[i]
		objGVK := obj.GroupVersionKind()

		if _, skip := unremovables[objGVK]; skip {
			continue
		}

		l.Info("cleanup excluded chart resource",
			"gvk", objGVK,
			"ns", obj.GetNamespace(),
			"name", obj.GetName(),
		)

		if err := rr.Client.Delete(ctx, obj, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
			if k8serr.IsNotFound(err) {
				continue
			}

			return fmt.Errorf("cleanup delete failed for %s/%s: %w", obj.GetNamespace(), obj.GetName(), err)
		}
	}

	return nil
}
