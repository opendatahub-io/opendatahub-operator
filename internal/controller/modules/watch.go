package modules

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
)

// SetupModuleWatches registers a watch for each module's CR GVK on the given
// controller. This must be called after Build() because it needs the built
// controller to add dynamic watches. CRDs that don't yet exist are silently
// skipped; the module operator will deploy them, and the next reconcile will
// retry the watch setup.
func SetupModuleWatches(ctx context.Context, mgr ctrl.Manager, c controller.Controller) error {
	reg := DefaultRegistry()
	if !reg.HasEntries() {
		return nil
	}

	return reg.ForEach(func(handler ModuleHandler) error {
		if err := WatchModuleCR(ctx, mgr, handler.GetGVK(), c); err != nil {
			return fmt.Errorf("failed to watch module CR for %s: %w", handler.GetName(), err)
		}

		return nil
	})
}

// WatchModuleCR registers a watch on the given module CR GVK that maps status
// changes back to a DSC reconcile request. This ensures the DSC controller is
// re-queued whenever a module operator updates its CR status.
func WatchModuleCR(_ context.Context, mgr ctrl.Manager, gvk schema.GroupVersionKind, c controller.Controller) error {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)

	return c.Watch(
		source.Kind(mgr.GetCache(), u, handler.TypedEnqueueRequestsFromMapFunc(
			func(ctx context.Context, _ *unstructured.Unstructured) []reconcile.Request {
				return watchDataScienceClusters(ctx, mgr.GetClient())
			},
		)),
	)
}

// watchDataScienceClusters lists all DSC instances and returns reconcile requests
// for each. This mirrors the pattern used elsewhere in the DSC controller.
func watchDataScienceClusters(ctx context.Context, cli client.Client) []reconcile.Request {
	instanceList := &dscv2.DataScienceClusterList{}
	if err := cli.List(ctx, instanceList); err != nil {
		return nil
	}

	requests := make([]reconcile.Request, len(instanceList.Items))
	for i := range instanceList.Items {
		requests[i] = reconcile.Request{
			NamespacedName: types.NamespacedName{Name: instanceList.Items[i].Name},
		}
	}

	return requests
}
