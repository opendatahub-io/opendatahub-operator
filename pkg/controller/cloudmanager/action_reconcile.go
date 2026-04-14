package cloudmanager

import (
	"context"
	"errors"
	"fmt"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/helm"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/status/deployments"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

var ConditionsTypes = []string{
	status.ConditionDeploymentsAvailable,
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

// NewReconcileAction creates a combined action that:
// - Renders Helm charts
// - Runs PreApply hooks from HelmCharts
// - Deploys resources via SSA
// - Runs PostApply hooks from HelmCharts
// - Checks deployment status.
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
		deploy.WithPartOfLabel(labels.InfrastructurePartOf),
		deploy.WithAnnotationPrefix(labels.ODHInfrastructurePrefix),
	)...)
	deploymentsAction := deployments.NewAction(
		deployments.InNamespaceFn(func(_ context.Context, _ *types.ReconciliationRequest) (string, error) {
			return "", nil
		}),
		deployments.WithPartOfLabel(labels.InfrastructurePartOf),
		deployments.WithSelectorLabel(labels.InfrastructurePartOf, action.resourceID),
	)

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

		// Check deployments
		// TODO: the monitoring should be set per dependency to improve user experience
		if err := deploymentsAction(ctx, rr); err != nil {
			return fmt.Errorf("deployments check failed: %w", err)
		}

		return nil
	}, nil
}
