package modules

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
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
		ensureDashboardNamespacedRBAC,
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
		WithDynamicOwnership(reconciler.WithGVKPredicates(moduleStatusPredicates())).
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
			))).
		// Namespace CREATE events re-trigger reconciliation so that ensureDashboardNamespacedRBAC
		// can create the notebooks/model-registry Role and RoleBinding when the target
		// namespace appears after initial reconcile. UPDATE and DELETE are not needed:
		// deletions cascade automatically, and label/annotation changes don't affect RBAC provisioning.
		Watches(
			&corev1.Namespace{},
			reconciler.WithEventMapper(func(ctx context.Context, _ client.Object) []reconcile.Request {
				return cluster.WatchDataScienceClusters(ctx, mgr.GetClient())
			}),
			reconciler.WithPredicates(predicate.Funcs{
				CreateFunc:  func(_ event.CreateEvent) bool { return true },
				UpdateFunc:  func(_ event.UpdateEvent) bool { return false },
				DeleteFunc:  func(_ event.DeleteEvent) bool { return false },
				GenericFunc: func(_ event.GenericEvent) bool { return false },
			}))

	b = addModuleCRWatches(b)
	b = addModuleCRDWatches(b, func(ctx context.Context, _ client.Object) []reconcile.Request {
		return cluster.WatchDataScienceClusters(ctx, mgr.GetClient())
	})

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
		WithDynamicOwnership(reconciler.WithGVKPredicates(moduleStatusPredicates())).
		WithoutConditionCleanup().
		WithoutStatusConditionsIf(cr.HasEntries).
		WithAction(enableModulesFromPlatform)

	reg := DefaultRegistry()
	if reg.HasEntries() {
		if err := reg.ForAll(func(h ModuleHandler, _ bool) error {
			b = b.WatchesGVK(h.GetGVK(),
				reconciler.Dynamic(reconciler.CrdExists(h.GetGVK())),
				reconciler.WithEventMapper(
					func(ctx context.Context, _ client.Object) []reconcile.Request {
						return cluster.WatchPlatforms(ctx, mgr.GetClient())
					}),
				reconciler.WithPredicates(
					dependentpredicates.New(dependentpredicates.WithWatchStatus(true)),
				),
			)
			return nil
		}); err != nil {
			return fmt.Errorf("failed to register module GVK watches: %w", err)
		}
	}

	b = addModuleCRDWatches(b, func(ctx context.Context, _ client.Object) []reconcile.Request {
		return cluster.WatchPlatforms(ctx, mgr.GetClient())
	})

	for _, a := range commonActions() {
		b = b.WithAction(a)
	}

	rec, err := b.WithConditions(
		status.ConditionTypeModulesReady,
		status.ConditionTypeProvisioningProgress,
	).Build(ctx)
	if err != nil {
		return fmt.Errorf("failed to create module reconciler (platform mode): %w", err)
	}
	registerModuleCROwnedTypes(rec)
	return nil
}

// addModuleCRDWatches registers a non-dynamic watch on CustomResourceDefinition
// resources filtered to the API groups used by registered modules. When a module
// CRD is created, this triggers a reconcile so the controller can register the
// dynamic module CR watch and read the module's status — resolving the race
// where the controller reconciles before a module CRD is available.
func addModuleCRDWatches[T common.PlatformObject](
	b *reconciler.ReconcilerBuilder[T],
	mapper func(ctx context.Context, obj client.Object) []reconcile.Request,
) *reconciler.ReconcilerBuilder[T] {
	reg := DefaultRegistry()
	if !reg.HasEntries() {
		return b
	}

	groupSet := make(map[string]bool)
	_ = reg.ForAll(func(h ModuleHandler, _ bool) error {
		groupSet[h.GetGVK().Group] = true
		return nil
	})

	groups := make([]string, 0, len(groupSet))
	for g := range groupSet {
		groups = append(groups, g)
	}
	sort.Strings(groups)

	crdPredicates := make([]predicate.Predicate, 0, len(groups))
	for _, g := range groups {
		crdPredicates = append(crdPredicates, resources.CreatedOrUpdatedOrDeletedNameSuffixed("."+g))
	}

	b.Watches(
		&apiextensionsv1.CustomResourceDefinition{},
		reconciler.WithEventMapper(mapper),
		reconciler.WithPredicates(predicate.Or(crdPredicates...)),
	)

	return b
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

// AddModuleCRDWatches registers a non-dynamic CRD watch so that module CRD
// creation triggers a reconcile. Exported for use by the DSC controller.
func AddModuleCRDWatches[T common.PlatformObject](
	b *reconciler.ReconcilerBuilder[T],
	mapper func(ctx context.Context, obj client.Object) []reconcile.Request,
) *reconciler.ReconcilerBuilder[T] {
	return addModuleCRDWatches(b, mapper)
}

// AddDSCCompatibilityProjectorWatches registers watches for module CR status
// changes that must requeue the user-facing DSC controller.
//
// The datasciencecluster controller computes generic module readiness
// (ModulesReady, AIGatewayReady, etc.) via ComputeModulesStatus. DSC must
// watch every registered module CR so its status stays current as module
// CRs transition.
func AddDSCCompatibilityProjectorWatches[T common.PlatformObject](b *reconciler.ReconcilerBuilder[T]) *reconciler.ReconcilerBuilder[T] {
	reg := DefaultRegistry()
	if !reg.HasEntries() {
		return b
	}

	_ = reg.ForAll(func(handler ModuleHandler, _ bool) error {
		// Requeue the DSC controller from module CR status changes without
		// claiming ownership of the module CR type itself. The modules
		// controller provisions module CRs; the DSC controller only needs the
		// watch so its generic module status and any compatibility projections
		// stay current as module CRs transition.
		b.WatchesGVK(
			handler.GetGVK(),
			reconciler.Dynamic(reconciler.CrdExists(handler.GetGVK())),
			reconciler.WithPredicates(dependentpredicates.New(dependentpredicates.WithWatchStatus(true))),
		)
		return nil
	})

	return b
}
