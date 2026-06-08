package modules

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
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
// behavior. Dynamic ownership is enabled so all deployed resources
// (module CRs, operator Deployments, RBAC) get owner references pointing
// to the DSC. This provides cascade deletion and enables
// EnqueueRequestForOwner watches registered automatically by the
// dynamic ownership action.
func newDSCModuleReconciler(ctx context.Context, mgr ctrl.Manager) error {
	b := reconciler.ReconcilerFor(mgr, &dscv2.DataScienceCluster{}).
		WithInstanceName("modules").
		WithDynamicOwnership().
		WithoutConditionCleanup().
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

	registerModuleCROwnedTypes(rec)

	return nil
}

// newPlatformModuleReconciler creates the module controller in platform mode
// (xKS). It reconciles the Platform CR as its primary resource. No DSC or DSCI
// is available; only modules with ManagementState Managed are enabled.
// Dynamic ownership is enabled for the same reasons as DSC mode.
func newPlatformModuleReconciler(ctx context.Context, mgr ctrl.Manager) error {
	available, err := cluster.IsAPIAvailable(mgr.GetClient(), gvk.Platform)
	if err != nil {
		return fmt.Errorf("failed to check Platform CRD availability: %w", err)
	}

	if !available {
		logf.FromContext(ctx).Info(
			"Platform CRD not found, skipping module reconciler in platform mode. " +
				"Install the Platform CRD to enable module reconciliation.")
		return nil
	}

	b := reconciler.ReconcilerFor(mgr, &configv1alpha1.Platform{}).
		WithInstanceName("modules").
		WithDynamicOwnership().
		WithoutConditionCleanup().
		WithAction(enableModulesFromPlatform)

	for _, a := range commonActions() {
		b = b.WithAction(a)
	}

	rec, err := b.WithConditions(status.ConditionTypeModulesReady).Build(ctx)
	if err != nil {
		return fmt.Errorf("failed to create module reconciler (platform mode): %w", err)
	}

	registerModuleCROwnedTypes(rec)

	return nil
}
