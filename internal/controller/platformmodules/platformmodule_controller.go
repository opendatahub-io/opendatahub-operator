// Package platformmodules implements the controller that reconciles a
// single PlatformModule CR: it deploys one module's operator once the
// platform's DAG orchestrator has cleared its runlevel, mirrors the
// module CR's Ready condition, and runs two-phase cleanup on deletion.
//
// The Platform controller (internal/controller/modules) owns the DAG
// workflow: it creates/deletes PlatformModule CRs based on
// Platform.Spec.Modules[name].ManagementState and marks runlevels cleared
// in Platform status as batches become ready. This package only
// manages a single module's operator lifecycle; it never touches
// Platform.Spec.Modules or the module CR itself (module CRs remain owned
// by the DSC/DSCI controllers).
package platformmodules

import (
	"context"
	"fmt"

	semver "github.com/blang/semver/v4"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	helmrender "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/helm"
	kustomizerender "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/kustomize"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/gates"
	dependentpredicates "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/dependent"
	resourcepredicates "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
)

// NewPlatformModuleReconciler creates the controller that reconciles
// PlatformModule CRs, one instance per registered module.
func NewPlatformModuleReconciler(ctx context.Context, mgr ctrl.Manager) error {
	b := reconciler.ReconcilerFor(mgr, &configApi.PlatformModule{}).
		WithInstanceName("platformmodule").
		WithDynamicOwnership(reconciler.ExcludeGVKs(gvk.Lease)).
		WithPostStatusFn(protectPlatformModuleInventory).
		WatchesGVK(
			gvk.Platform,
			reconciler.WithEventHandler(platformDAGAdvanceHandler(mgr.GetScheme())),
			// The default watch filters ignore status changes and require a part-of
			// label. DAG advancement is a status-only event on the shared Platform.
			reconciler.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		WatchesGVK(
			gvk.ConfigMap,
			reconciler.WithEventMapper(func(ctx context.Context, _ client.Object) []reconcile.Request {
				return registeredPlatformModuleRequests(ctx, mgr.GetClient())
			}),
			reconciler.WithPredicates(predicate.Or(
				resourcepredicates.CreatedOrUpdatedOrDeletedNamed(gates.AcksConfigMap),
				resourcepredicates.CreatedOrUpdatedOrDeletedLabeled(gates.UpgradeGateLabel, "true"),
			)),
		).
		WithFinalizer(handlePlatformModuleDeletion).
		WithAction(checkPlatformModuleRunlevelGate).
		WithAction(provisionModuleOperator).
		WithAction(helmrender.NewAction()).
		WithAction(kustomizerender.NewAction()).
		WithAction(provision.ExtractUpgradeGates).
		WithAction(checkModuleUpgradeGates).
		WithAction(injectModuleEnv).
		WithAction(injectModulePlatformConfig).
		WithAction(recordIntendedInventory).
		WithAction(deploy.NewAction(
			// Migrate SSA ownership from datasciencecluster on upgrades from 3.5
			// and from platform on upgrades from 3.6.
			// TODO: remove the legacy once upgrade from 3.5 is no longer supported
			deploy.WithPreviousFieldOwners("datasciencecluster", "platform"),
			deploy.WithCache(),
			deploy.WithApplyOrder(),
			deploy.WithContinueOnError(),
		)).
		WithAction(updatePlatformModuleStatus)

	// Re-render Monitoring when its Collector webhook or ServiceMonitor API changes.
	b = b.
		Watches(&admissionregistrationv1.ValidatingWebhookConfiguration{},
			reconciler.WithEventMapper(monitoringModuleRequests),
			reconciler.WithPredicates(otelCollectorWebhookPredicate()),
		).
		Watches(&apiextensionsv1.CustomResourceDefinition{},
			reconciler.WithEventMapper(monitoringModuleRequests),
			reconciler.WithPredicates(resourcepredicates.CreatedOrUpdatedOrDeletedNamed(gvk.ServiceMonitorCRDName)),
		)

	// Watches are built once; register every handler even when currently disabled.
	if err := modules.DefaultRegistry().ForAll(func(handler modules.ModuleHandler, _ bool) error {
		name := handler.GetName()

		b = b.WatchesGVK(handler.GetGVK(),
			reconciler.Dynamic(reconciler.CrdExists(handler.GetGVK())),
			reconciler.WithEventMapper(func(_ context.Context, _ client.Object) []reconcile.Request {
				return []reconcile.Request{{NamespacedName: client.ObjectKey{Name: name}}}
			}),
			reconciler.WithPredicates(dependentpredicates.New(dependentpredicates.WithWatchStatus(true))),
		)

		return nil
	}); err != nil {
		return fmt.Errorf("failed to register module CR watches: %w", err)
	}

	b = modules.AddModuleCRDWatches(b, func(ctx context.Context, _ client.Object) []reconcile.Request {
		return registeredPlatformModuleRequests(ctx, mgr.GetClient())
	})

	_, err := b.WithConditions(reconciler.DependentConditions(
		status.ConditionTypeModuleCRReady,
		status.ConditionDeploymentsAvailable,
	)...).Build(ctx)
	if err != nil {
		return fmt.Errorf("failed to create platformmodule controller: %w", err)
	}

	return nil
}

func platformDAGAdvanceHandler(scheme *runtime.Scheme) handler.EventHandler {
	return handler.Funcs{
		UpdateFunc: func(ctx context.Context, e event.TypedUpdateEvent[client.Object], q workqueue.TypedRateLimitingInterface[reconcile.Request]) {
			// Framework watches deliver unstructured objects even for typed GVKs.
			oldPlatform, newPlatform := &configApi.Platform{}, &configApi.Platform{}
			if err := scheme.Convert(e.ObjectOld, oldPlatform, nil); err != nil {
				logf.FromContext(ctx).Error(err, "decoding previous Platform DAG status")
				return
			}
			if err := scheme.Convert(e.ObjectNew, newPlatform, nil); err != nil {
				logf.FromContext(ctx).Error(err, "decoding current Platform DAG status")
				return
			}

			for _, request := range platformDAGAdvanceRequests(oldPlatform, newPlatform) {
				q.Add(request)
			}
		},
	}
}

// platformDAGAdvanceRequests wakes the trackers eligible for reconciliation:
// on a new release every module in a cleared batch (a runlevel timeout can
// re-clear several batches at once), then only newly opened runlevels.
// Fanning out every Platform status update would create a tracker ->
// Platform status -> tracker reconciliation loop.
func platformDAGAdvanceRequests(oldPlatform, newPlatform *configApi.Platform) []reconcile.Request {
	if oldPlatform == nil || newPlatform == nil {
		return nil
	}

	oldDAG, newDAG := oldPlatform.Status.DAG, newPlatform.Status.DAG
	if newDAG.Release.Version.Equals(semver.Version{}) {
		return nil
	}

	newRelease := oldDAG.Release.Version.String() != newDAG.Release.Version.String() || oldDAG.Release.Name != newDAG.Release.Name
	if !newRelease && newDAG.ClearedRunlevel <= oldDAG.ClearedRunlevel {
		return nil
	}

	batches, err := provision.DefaultRegistry().ResolvedBatches()
	if err != nil {
		return nil
	}

	// Ordinary Platform status changes must not fan out into every tracker.
	// Do not filter runtime enablement: a deleting tracker still needs retries.
	var requests []reconcile.Request
	for _, batch := range batches {
		for _, node := range provision.ModulesInBatch(batch) {
			order := node.GetRunlevel().Order
			if !modules.DefaultRegistry().IsEnabled(node.GetName()) || order > newDAG.ClearedRunlevel {
				continue
			}
			if !newRelease && order <= oldDAG.ClearedRunlevel {
				continue
			}
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKey{Name: node.GetName()}})
		}
	}

	return requests
}

func registeredPlatformModuleRequests(ctx context.Context, cli client.Client) []reconcile.Request {
	var list configApi.PlatformModuleList
	if err := cli.List(ctx, &list); err != nil {
		logf.FromContext(ctx).Error(err, "listing PlatformModule trackers for event fanout")
		return nil
	}

	requests := make([]reconcile.Request, 0, len(list.Items))
	for i := range list.Items {
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKey{Name: list.Items[i].GetName()}})
	}
	return requests
}
