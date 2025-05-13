package handlers

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

func LabelToName(key string) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, a client.Object) []reconcile.Request {
		values := a.GetLabels()
		if len(values) == 0 {
			return []reconcile.Request{}
		}

		name := values[key]
		if name == "" {
			return []reconcile.Request{}
		}

		return []reconcile.Request{{
			NamespacedName: types.NamespacedName{
				Name: name,
			},
		}}
	})
}
func AnnotationToName(key string) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		values := obj.GetAnnotations()
		if len(values) == 0 {
			return []reconcile.Request{}
		}

		name := values[key]
		if name == "" {
			return []reconcile.Request{}
		}

		return []reconcile.Request{{
			NamespacedName: types.NamespacedName{
				Name: name,
			},
		}}
	})
}

func Fn(fn func(ctx context.Context, a client.Object) []reconcile.Request) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(fn)
}

func ToNamed(name string) handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, a client.Object) []reconcile.Request {
		return []reconcile.Request{{
			NamespacedName: types.NamespacedName{
				Name: name,
			},
		}}
	})
}

func RequestFromObject() handler.EventHandler {
	return Fn(func(ctx context.Context, obj client.Object) []reconcile.Request {
		return []reconcile.Request{{
			NamespacedName: resources.NamespacedNameFromObject(obj),
		}}
	})
}

func ToAddonParamReq() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, obj client.Object) []reconcile.Request {
		operatorNs, err := cluster.GetOperatorNamespace()
		if err != nil {
			return nil
		}

		if obj.GetName() == "addon-managed-odh-parameters" && obj.GetNamespace() == operatorNs {
			return []reconcile.Request{{
				NamespacedName: types.NamespacedName{
					Name:      "addon-managed-odh-parameters",
					Namespace: operatorNs,
				},
			}}
		}
		return nil
	})
}

// NewEventHandlerForGVK creates an event handler that watches for events on resources of the specified GroupVersionKind.
// It uses the provided client to list resources and applies any additional list options.
//
// Parameters:
//   - cli: The Kubernetes client used to list resources
//   - gvk: The GroupVersionKind to watch for events
//   - options: Optional list options to filter the resources (e.g., namespace, label selectors)
//
// Returns:
//   - handler.EventHandler: An event handler that can be used with controller-runtime's controller.Watch
//
// Example:
//
//	gvk := schema.GroupVersionKind{
//		Group:   "apps",
//		Version: "v1",
//		Kind:    "Deployment",
//	}
//
//	ctrl.Watch(
//		&source.Kind{Type: &appsv1.Deployment{}},
//		NewEventHandlerForGVK(
//			cli,
//			gvk,
//		),
//	)
func NewEventHandlerForGVK(
	cli client.Client,
	gvk schema.GroupVersionKind,
	options ...client.ListOption,
) handler.EventHandler {
	return Fn(func(ctx context.Context, _ client.Object) []reconcile.Request {
		list := unstructured.UnstructuredList{}
		list.SetGroupVersionKind(gvk)

		err := cli.List(ctx, &list, options...)
		switch {
		case err != nil:
			return []reconcile.Request{}
		case len(list.Items) == 0:
			return []reconcile.Request{}
		default:
			requests := make([]reconcile.Request, len(list.Items))
			for i := range list.Items {
				requests[i] = reconcile.Request{
					NamespacedName: resources.NamespacedNameFromObject(&list.Items[i]),
				}
			}

			return requests
		}
	})
}
