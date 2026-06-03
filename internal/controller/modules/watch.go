package modules

import (
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
)

// OwnedTypeRegistrar allows registering GVKs as statically owned types
// on a controller. The reconciler's *reconciler.Reconciler satisfies
// this interface.
type OwnedTypeRegistrar interface {
	AddOwnedType(gvk schema.GroupVersionKind)
}

// registerModuleCROwnedTypes registers each module's CR GVK as a statically
// owned type on the reconciler. This ensures the GC action's type predicate
// (which checks rr.Controller.Owns()) returns true for module CRs from the
// first reconcile, before dynamic ownership has a chance to discover them.
//
// Watch registration is handled automatically by the dynamic ownership action
// (enabled via WithDynamicOwnership on the builder). The dynamic ownership
// action uses EnqueueRequestForOwner so module CR status changes trigger
// reconciliation of the owning DSC/Platform.
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
