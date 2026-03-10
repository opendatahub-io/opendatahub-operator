package cloudmanager

import (
	"context"
	"fmt"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/helm"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// reconcileAction holds the configuration for the combined reconcile action.
type reconcileAction struct {
	helmOpts   []helm.ActionOpts
	deployOpts []deploy.ActionOpts
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

// NewReconcileAction creates a combined action that:
// 1. Renders Helm charts
// 2. Runs PreApply hooks from HelmCharts
// 3. Deploys resources via SSA
// 4. Runs PostApply hooks from HelmCharts.
func NewReconcileAction(opts ...ReconcileActionOpts) actions.Fn {
	action := reconcileAction{}

	for _, opt := range opts {
		opt(&action)
	}

	helmRender := helm.NewAction(action.helmOpts...)
	deployAction := deploy.NewAction(action.deployOpts...)

	return func(ctx context.Context, rr *types.ReconciliationRequest) error {
		// Render Helm charts (populates rr.Resources)
		if err := helmRender(ctx, rr); err != nil {
			return fmt.Errorf("helm render failed: %w", err)
		}

		// Execute pre-apply hooks
		err := runHooks(ctx, rr, func(c *types.HelmChartInfo) []types.HookFn {
			return c.PreApply
		})
		if err != nil {
			return err
		}

		// Set infrastructure label on all rendered resources
		kind, err := resources.KindForObject(rr.Client.Scheme(), rr.Instance)
		if err != nil {
			return fmt.Errorf("failed to get instance kind: %w", err)
		}
		for i := range rr.Resources {
			resources.SetLabel(&rr.Resources[i], labels.InfrastructurePartOf, labels.NormalizePartOfValue(kind))
		}

		// Deploy resources via SSA
		if err := deployAction(ctx, rr); err != nil {
			return fmt.Errorf("deploy failed: %w", err)
		}

		// Execute post-apply hooks
		err = runHooks(ctx, rr, func(c *types.HelmChartInfo) []types.HookFn {
			return c.PostApply
		})
		if err != nil {
			return err
		}

		return nil
	}
}
