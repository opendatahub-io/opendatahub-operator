package modules

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	helmrender "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/helm"
	kustomizerender "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/kustomize"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/gates"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates"
	dependentpredicates "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/dependent"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/flags"
)

// commonActions returns the shared action chain for both DSC and Platform modes.
//
// Ordering: provisionModules and render run before the gate check so
// that gate ConfigMaps embedded in module Helm charts are discovered
// before the check runs. ExtractUpgradeGates pulls gate CMs out of
// rr.Resources and stashes them on rr.GateEntries. checkUpgradeGates
// then merges all gate sources and writes descriptions to
// odh-upgrade-acks. If unacked gates exist, deploy never runs.
func commonActions() []actions.Fn {
	return []actions.Fn{
		initializeModules,
		cleanupDisabledModules,
		provisionModules,
		helmrender.NewAction(),
		kustomizerender.NewAction(),
		provision.ExtractUpgradeGates,
		checkUpgradeGates,
		injectModuleEnv,
		injectPlatformConfig,
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
		// The modules controller still reconciles against the DSC in OpenShift mode,
		// but the datasciencecluster controller must remain the only writer of the
		// user-facing DSC status conditions. Letting both controllers patch DSC
		// conditions would reintroduce atomic status.conditions races where the
		// last writer wins.
		WithoutConditionCleanup().
		WithoutStatusConditionsIf(cr.HasEntries).
		Watches(
			&dsciv2.DSCInitialization{},
			reconciler.WithEventMapper(func(ctx context.Context, _ client.Object) []reconcile.Request {
				return cluster.WatchDataScienceClusters(ctx, mgr.GetClient())
			}),
			reconciler.WithPredicates(predicates.DefaultPredicate)).
		Watches(
			&corev1.ConfigMap{},
			reconciler.WithEventMapper(func(ctx context.Context, _ client.Object) []reconcile.Request {
				return cluster.WatchDataScienceClusters(ctx, mgr.GetClient())
			}),
			reconciler.WithPredicates(predicate.Or(
				resources.CreatedOrUpdatedOrDeletedNamed(gates.AcksConfigMap),
				resources.CreatedOrUpdatedOrDeletedLabeled(gates.UpgradeGateLabel, "true"),
			)))

	b = addModuleCRWatches(b)

	for _, a := range commonActions() {
		b = b.WithAction(a)
	}

	if _, err := b.WithConditions(status.ConditionTypeModulesReady).Build(ctx); err != nil {
		return fmt.Errorf("failed to create module reconciler (DSC mode): %w", err)
	}
	return nil
}

// newPlatformModuleReconciler creates the module controller in platform mode
// (xKS). It reconciles the Platform CR as its primary resource. No DSC or DSCI
// is available; only modules with ManagementState Managed are enabled.
// Dynamic ownership is enabled for the same reasons as DSC mode.
func newPlatformModuleReconciler(ctx context.Context, mgr ctrl.Manager) error {
	b := reconciler.ReconcilerFor(mgr, &configv1alpha1.Platform{}).
		WithInstanceName("modules").
		WithDynamicOwnership().
		WithoutConditionCleanup().
		WithoutStatusConditionsIf(cr.HasEntries).
		WithAction(enableModulesFromPlatform)

	b = addModuleCRWatches(b)

	for _, a := range commonActions() {
		b = b.WithAction(a)
	}

	if _, err := b.WithConditions(status.ConditionTypeModulesReady).Build(ctx); err != nil {
		return fmt.Errorf("failed to create module reconciler (platform mode): %w", err)
	}
	return nil
}

func addModuleCRWatches[T common.PlatformObject](b *reconciler.ReconcilerBuilder[T]) *reconciler.ReconcilerBuilder[T] {
	reg := DefaultRegistry()
	if !reg.HasEntries() {
		return b
	}

	_ = reg.ForAll(func(handler ModuleHandler, _ bool) error {
		b.OwnsGVK(
			handler.GetGVK(),
			reconciler.Dynamic(reconciler.CrdExists(handler.GetGVK())),
			reconciler.WithPredicates(dependentpredicates.New(dependentpredicates.WithWatchStatus(true))),
		)
		return nil
	})

	return b
}

// AddDSCCompatibilityProjectorWatches registers watches for module CRs that
// feed compatibility status back into the user-facing DSC. Once DSC status
// ownership moved to the datasciencecluster controller, those same module CR
// status changes must also requeue the DSC controller.
func AddDSCCompatibilityProjectorWatches[T common.PlatformObject](b *reconciler.ReconcilerBuilder[T]) *reconciler.ReconcilerBuilder[T] {
	reg := DefaultRegistry()
	if !reg.HasEntries() {
		return b
	}

	_ = reg.ForAll(func(handler ModuleHandler, _ bool) error {
		if _, ok := handler.(DSCStatusProjector); !ok {
			return nil
		}
		// Requeue the DSC controller from module CR status changes without
		// claiming ownership of the module CR type itself. The modules
		// controller provisions module CRs; the DSC controller only needs the
		// watch for compatibility-status projection.
		b.WatchesGVK(
			handler.GetGVK(),
			reconciler.Dynamic(reconciler.CrdExists(handler.GetGVK())),
			reconciler.WithPredicates(dependentpredicates.New(dependentpredicates.WithWatchStatus(true))),
		)
		return nil
	})

	return b
}
