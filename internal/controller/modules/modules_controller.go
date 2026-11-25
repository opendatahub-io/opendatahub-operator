package modules

import (
	"context"
	"fmt"
	"sort"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/gates"
	dependentpredicates "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/dependent"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
)

// NewModuleReconciler creates the platform controller that reconciles the
// Platform CR. On OpenShift the DSC/DSCI controllers project module
// enablement into Platform CR via SSA; on xKS the Helm chart creates it.
// The controller always reads Platform CR — single code path.
func NewModuleReconciler(ctx context.Context, mgr ctrl.Manager) error {
	platformRequest := []reconcile.Request{{NamespacedName: k8stypes.NamespacedName{Name: configApi.PlatformInstanceName}}}
	// TODO: Remove this migration once upgrades from 3.6 are no longer supported.
	migration := newLegacyMigration()

	b := reconciler.ReconcilerFor(mgr, &configApi.Platform{}).
		WithInstanceName("modules").
		WithDynamicOwnership(
			reconciler.WithGVKPredicates(map[schema.GroupVersionKind][]predicate.Predicate{
				gvk.PlatformModule: {
					dependentpredicates.New(dependentpredicates.WithWatchStatus(true)),
				},
			}),
		).
		WatchesGVK(
			gvk.ConfigMap,
			reconciler.WithEventMapper(func(_ context.Context, _ client.Object) []reconcile.Request {
				return platformRequest
			}),
			reconciler.WithPredicates(predicate.Or(
				resources.CreatedOrUpdatedOrDeletedNamed(gates.AcksConfigMap),
				resources.CreatedOrUpdatedOrDeletedLabeled(gates.UpgradeGateLabel, "true"),
			)),
		).
		WithAction(checkPlatformModuleUpgradeGates).
		WithAction(cleanupDisabledPlatformModules).
		WithAction(provisionPlatformModules).
		WithAction(deploy.NewAction(
			deploy.WithCache(),
			deploy.WithApplyOrder(),
			deploy.WithContinueOnError(),
		)).
		WithAction(coordinateDAG).
		WithAction(aggregatePlatformModulesStatus).
		WithAction(migration.run)

	statusPredicate := dependentpredicates.New(dependentpredicates.WithWatchStatus(true))

	if err := cr.DefaultRegistry().ForEach(func(handler cr.ComponentHandler) error {
		b = b.WatchesGVK(handler.GroupVersionKind(),
			reconciler.Dynamic(reconciler.CrdExists(handler.GroupVersionKind())),
			reconciler.WithEventMapper(func(_ context.Context, _ client.Object) []reconcile.Request {
				return platformRequest
			}),
			reconciler.WithPredicates(statusPredicate),
		)
		return nil
	}); err != nil {
		return fmt.Errorf("failed to register component CR watches: %w", err)
	}

	b = addModuleCRDWatches(b, func(ctx context.Context, _ client.Object) []reconcile.Request {
		return cluster.WatchDataScienceClusters(ctx, mgr.GetClient())
	})

	// Hold Platform until its owned trackers have finished operator cleanup.
	_, err := b.WithFinalizer(finalizePlatform).
		WithConditions(reconciler.DependentConditions(
			status.ConditionTypeModulesReady, status.ConditionTypeProvisioningProgress,
		)...).Build(ctx)
	if err != nil {
		return fmt.Errorf("failed to create platform controller: %w", err)
	}

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
// (ModulesReady, AIGatewayReady, etc.) via ComputeModulesStatusDetailed for
// ConfigFromDSC modules only. DSCI-configured modules (e.g. monitoring) report
// status on DSCI and are watched there.
func AddDSCCompatibilityProjectorWatches[T common.PlatformObject](b *reconciler.ReconcilerBuilder[T]) *reconciler.ReconcilerBuilder[T] {
	reg := DefaultRegistry()
	if !reg.HasEntries() {
		return b
	}

	_ = reg.ForConfigSource(ConfigFromDSC, func(handler ModuleHandler, _ bool) error {
		// Requeue the DSC controller from module CR status changes without
		// claiming ownership of the module CR type itself. The DSC controller
		// owns the CR and needs the watch so its generic module status and
		// compatibility projections
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
