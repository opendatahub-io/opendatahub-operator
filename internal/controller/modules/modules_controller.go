package modules

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	helmrender "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/helm"
	kustomizerender "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/kustomize"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// NewModuleReconciler creates a dedicated controller for the module lifecycle.
// It reconciles DataScienceCluster as its primary resource (same as the DSC
// component controller) but uses a distinct controller name ("modules") and a
// completely separate action pipeline.
//
// The module reconciler watches both DSC and DSCI so it can react to changes
// in either. This is the foundation for a future disconnect from DSC/DSCI --
// when those CRDs are deprecated, only this controller's primary resource and
// event sources need to change; all module handler logic stays untouched.
func NewModuleReconciler(ctx context.Context, mgr ctrl.Manager) error {
	rec, err := reconciler.ReconcilerFor(mgr, &dscv2.DataScienceCluster{}).
		WithInstanceName("modules").
		Watches(
			&dsciv2.DSCInitialization{},
			reconciler.WithEventMapper(func(ctx context.Context, _ client.Object) []reconcile.Request {
				return watchDataScienceClusters(ctx, mgr.GetClient())
			}),
			reconciler.WithPredicates(predicates.DefaultPredicate)).
		WithAction(initializeModules).
		WithAction(cleanupDisabledModules).
		WithAction(provisionModules).
		WithAction(helmrender.NewAction()).
		WithAction(kustomizerender.NewAction()).
		WithAction(injectModuleEnv).
		WithAction(deploy.NewAction(
			deploy.WithCache(),
			deploy.WithApplyOrder()),
		).
		WithAction(updateModuleStatus).
		WithAction(gc.NewAction(
			gc.WithTypePredicate(
				func(rr *types.ReconciliationRequest, objGVK schema.GroupVersionKind) (bool, error) {
					return rr.Controller.Owns(objGVK), nil
				},
			),
		)).
		WithConditions(status.ConditionTypeModulesReady).
		Build(ctx)

	if err != nil {
		return fmt.Errorf("failed to create module reconciler: %w", err)
	}

	if err := SetupModuleWatches(ctx, mgr, rec.Controller, rec); err != nil {
		return fmt.Errorf("failed to set up module watches: %w", err)
	}

	return nil
}
