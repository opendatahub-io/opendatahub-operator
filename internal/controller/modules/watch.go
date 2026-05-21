package modules

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	configv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

// ModuleCRMapper maps a module CR event to reconcile requests for the
// primary resource (DSC in OpenShift mode, Platform in xKS mode).
type ModuleCRMapper = handler.TypedMapFunc[*unstructured.Unstructured, reconcile.Request]

// DSCMapper returns a mapper that lists all DSC instances and enqueues them.
// Used in DSC mode (OpenShift/ODH).
func DSCMapper(cli client.Client) ModuleCRMapper {
	return func(ctx context.Context, _ *unstructured.Unstructured) []reconcile.Request {
		return cluster.WatchDataScienceClusters(ctx, cli)
	}
}

// PlatformMapper returns a mapper that enqueues the Platform singleton.
// Used in xKS mode where there is no DSC.
func PlatformMapper() ModuleCRMapper {
	return func(_ context.Context, _ *unstructured.Unstructured) []reconcile.Request {
		return []reconcile.Request{{
			NamespacedName: types.NamespacedName{
				Name: configv1alpha1.PlatformInstanceName,
			},
		}}
	}
}

// SetupModuleWatches registers a watch for each module's CR GVK on the given
// controller and registers the GVK as an owned type via owner.AddOwnedType.
// Ownership registration enables the deploy action to set the primary resource
// as controller owner of module CRs, and the GC action to delete module CRs
// when they are no longer in rr.Resources (i.e., when the module is disabled).
//
// The mapper function maps module CR events back to the primary resource.
//
// All registered modules (including CLI-disabled ones) are processed so that
// cleanup paths work correctly. CRDs that don't yet exist are silently
// skipped for the watch; the module operator will deploy them and the next
// reconcile will retry.
func SetupModuleWatches(mgr ctrl.Manager, c controller.Controller, owner OwnedTypeRegistrar, mapper ModuleCRMapper) error {
	reg := DefaultRegistry()
	if !reg.HasEntries() {
		return nil
	}

	return reg.ForAll(func(h ModuleHandler, _ bool) error {
		gvk := h.GetGVK()

		owner.AddOwnedType(gvk)

		if err := watchModuleCR(mgr, gvk, c, mapper); err != nil {
			if meta.IsNoMatchError(err) {
				return nil
			}
			return fmt.Errorf("failed to watch module CR for %s: %w", h.GetName(), err)
		}

		return nil
	})
}

// watchModuleCR registers a watch on the given module CR GVK that maps
// status changes back to the primary resource via the given mapper.
func watchModuleCR(mgr ctrl.Manager, gvk schema.GroupVersionKind, c controller.Controller, mapper ModuleCRMapper) error {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)

	return c.Watch(
		source.Kind(mgr.GetCache(), u, handler.TypedEnqueueRequestsFromMapFunc[*unstructured.Unstructured, reconcile.Request](mapper)),
	)
}
