package modules

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	helmrender "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/helm"
	kustomizerender "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/kustomize"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/gates"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/dependent"
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
		WithDynamicOwnership(reconciler.WithGVKPredicates(moduleStatusPredicates())).
		WithoutConditionCleanup().
		WithAction(enableModulesFromPlatform)

	platformRequest := []reconcile.Request{{NamespacedName: k8stypes.NamespacedName{Name: configv1alpha1.PlatformInstanceName}}}
	statusPredicate := dependent.New(dependent.WithWatchStatus(true))

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

	for _, a := range commonActions() {
		b = b.WithAction(a)
	}

	rec, err := b.WithConditions(
		status.ConditionTypeModulesReady,
		status.ConditionTypeProvisioningProgress,
	).Build(ctx)
	if err != nil {
		return fmt.Errorf("failed to create platform controller: %w", err)
	}

	registerModuleCROwnedTypes(rec)

	return nil
}
