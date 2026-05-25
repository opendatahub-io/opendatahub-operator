package modules

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	helmrender "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/helm"
	kustomizerender "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/kustomize"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/flags"
)

// commonActions returns the shared action chain for both DSC and Platform modes.
func commonActions() []actions.Fn {
	return []actions.Fn{
		initializeModules,
		cleanupDisabledModules,
		provisionModules,
		helmrender.NewAction(),
		kustomizerender.NewAction(),
		injectModuleEnv,
		deploy.NewAction(
			deploy.WithCache(),
			deploy.WithApplyOrder(),
			deploy.WithContinueOnError(),
		),
		updateModuleStatus,
		gc.NewAction(
			gc.WithTypePredicate(
				func(rr *types.ReconciliationRequest, objGVK schema.GroupVersionKind) (bool, error) {
					return rr.Controller.Owns(objGVK), nil
				},
			),
		),
	}
}

// NewModuleReconciler creates a dedicated controller for the module lifecycle.
// It checks the DSC/DSCI suppression flags to select the appropriate mode:
//
//   - DSC mode (OpenShift/ODH): reconciles DataScienceCluster as its primary
//     resource, watches DSCI, full PlatformContext available.
//   - Platform mode (xKS): reconciles a Platform CR as its primary resource.
//     PlatformContext.DSC and .DSCI are nil; only modules whose
//     ManagementState is Managed in spec.modules are enabled.
func NewModuleReconciler(ctx context.Context, mgr ctrl.Manager) error {
	if flags.IsDSCEnabled() && flags.IsDSCIEnabled() {
		return newDSCModuleReconciler(ctx, mgr)
	}

	return newPlatformModuleReconciler(ctx, mgr)
}

// newDSCModuleReconciler creates the module controller in DSC mode.
// It reconciles DataScienceCluster and watches DSCI, matching the original
// behavior.
func newDSCModuleReconciler(ctx context.Context, mgr ctrl.Manager) error {
	b := reconciler.ReconcilerFor(mgr, &dscv2.DataScienceCluster{}).
		WithInstanceName("modules").
		Watches(
			&dsciv2.DSCInitialization{},
			reconciler.WithEventMapper(func(ctx context.Context, _ client.Object) []reconcile.Request {
				return cluster.WatchDataScienceClusters(ctx, mgr.GetClient())
			}),
			reconciler.WithPredicates(predicates.DefaultPredicate))

	for _, a := range commonActions() {
		b = b.WithAction(a)
	}

	rec, err := b.WithConditions(status.ConditionTypeModulesReady).Build(ctx)
	if err != nil {
		return fmt.Errorf("failed to create module reconciler (DSC mode): %w", err)
	}

	if err := SetupModuleWatches(mgr, rec.Controller, rec, DSCMapper(mgr.GetClient())); err != nil {
		return fmt.Errorf("failed to set up module watches: %w", err)
	}

	return nil
}

// newPlatformModuleReconciler creates the module controller in platform mode
// (xKS). It reconciles the Platform CR as its primary resource. No DSC or DSCI
// is available; only modules with ManagementState Managed are enabled.
func newPlatformModuleReconciler(ctx context.Context, mgr ctrl.Manager) error {
	b := reconciler.ReconcilerFor(mgr, &configv1alpha1.Platform{}).
		WithInstanceName("modules").
		WithAction(enableModulesFromPlatform)

	for _, a := range commonActions() {
		b = b.WithAction(a)
	}

	rec, err := b.WithConditions(status.ConditionTypeModulesReady).Build(ctx)
	if err != nil {
		return fmt.Errorf("failed to create module reconciler (platform mode): %w", err)
	}

	if err := SetupModuleWatches(mgr, rec.Controller, rec, PlatformMapper()); err != nil {
		return fmt.Errorf("failed to set up module watches: %w", err)
	}

	return nil
}
