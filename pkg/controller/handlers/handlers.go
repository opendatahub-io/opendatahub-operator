package handlers

import (
	"context"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

func ToOwner() handler.EventHandler {
	return handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, a client.Object) []reconcile.Request {
		objLabels := a.GetLabels()
		if len(objLabels) == 0 {
			return []reconcile.Request{}
		}

		partOf := objLabels[labels.ComponentPartOf]
		if partOf == "" {
			return []reconcile.Request{}
		}

		return []reconcile.Request{{
			NamespacedName: types.NamespacedName{
				Name: partOf,
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
