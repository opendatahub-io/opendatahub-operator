package modules

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	helmrender "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/helm"
	kustomizerender "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/kustomize"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/gates"
	dependentpredicates "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/dependent"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
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

// NewModuleReconciler creates the platform controller that reconciles the
// Platform CR. On OpenShift the DSC/DSCI controllers project module
// enablement into Platform CR via SSA; on xKS the Helm chart creates it.
// The controller always reads Platform CR — single code path.
func NewModuleReconciler(ctx context.Context, mgr ctrl.Manager) error {
	b := reconciler.ReconcilerFor(mgr, &configv1alpha1.Platform{}).
		WithInstanceName("modules").
		WithDynamicOwnership().
		WithAction(enableModulesFromPlatform)

	platformRequest := []reconcile.Request{{NamespacedName: k8stypes.NamespacedName{Name: configv1alpha1.PlatformInstanceName}}}
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

	if err := DefaultRegistry().ForEach(func(handler ModuleHandler) error {
		b = b.WatchesGVK(handler.GetGVK(),
			reconciler.Dynamic(reconciler.CrdExists(handler.GetGVK())),
			reconciler.WithEventMapper(func(_ context.Context, _ client.Object) []reconcile.Request {
				return platformRequest
			}),
			reconciler.WithPredicates(statusPredicate),
		)
		return nil
	}); err != nil {
		return fmt.Errorf("failed to register module CR watches: %w", err)
	}

	b = b.
		Watches(
			&corev1.ConfigMap{},
			reconciler.WithEventMapper(func(_ context.Context, _ client.Object) []reconcile.Request {
				return []reconcile.Request{{NamespacedName: k8stypes.NamespacedName{Name: configv1alpha1.PlatformInstanceName}}}
			}),
			reconciler.WithPredicates(predicate.Or(
				resources.CreatedOrUpdatedOrDeletedNamed(gates.AcksConfigMap),
				resources.CreatedOrUpdatedOrDeletedLabeled(gates.UpgradeGateLabel, "true"),
			)))

	b = addModuleCRWatches(b)
	b = addModuleCRDWatches(b, func(ctx context.Context, _ client.Object) []reconcile.Request {
		return cluster.WatchDataScienceClusters(ctx, mgr.GetClient())
	})

	for _, a := range commonActions() {
		b = b.WithAction(a)
	}

	_, err := b.WithConditions(
		status.ConditionTypeModulesReady,
		status.ConditionTypeProvisioningProgress,
	).Build(ctx)
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
