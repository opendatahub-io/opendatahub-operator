package modules

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/dependent"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
)

// OwnedTypeRegistrar allows registering GVKs as statically owned types
// on a controller. The reconciler's *reconciler.Reconciler satisfies
// this interface.
type OwnedTypeRegistrar interface {
	AddOwnedType(gvk schema.GroupVersionKind)
}

// moduleStatusPredicates returns predicates that include status changes for every module CR.
// it explicitiy add predicates on the CR's status change which can be used for calls to reconcile
// ensure WatchDelete: true, WatchUpdate: true, WatchStatus: true.
func moduleStatusPredicates() map[schema.GroupVersionKind][]predicate.Predicate {
	reg := DefaultRegistry()
	modulesPredicate := make(map[schema.GroupVersionKind][]predicate.Predicate)

	_ = reg.ForAll(func(h ModuleHandler, _ bool) error {
		modulesPredicate[h.GetGVK()] = []predicate.Predicate{
			dependent.New(dependent.WithWatchStatus(true)),
		}
		return nil
	})

	return modulesPredicate
}

// registerModuleCROwnedTypes registers each module's CR GVK as a statically
// owned type on the reconciler. This ensures the GC action's type predicate
// (which checks rr.Controller.Owns()) returns true for module CRs from the
// first reconcile, before dynamic ownership has a chance to discover them.
//
// Watch registration is handled automatically by the dynamic ownership action
// (enabled via WithDynamicOwnership on the builder). The dynamic ownership
// action uses EnqueueRequestForOwner with status predicates so module CR status changes trigger reconciliation
// of the owning DSC/Platform.
func registerModuleCROwnedTypes(rec *reconciler.Reconciler) {
	reg := DefaultRegistry()
	if !reg.HasEntries() {
		return
	}

	_ = reg.ForAll(func(h ModuleHandler, _ bool) error {
		rec.AddOwnedType(h.GetGVK())
		return nil
	})
}
