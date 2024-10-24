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

		managedBy := objLabels[labels.ComponentManagedBy]
		if managedBy == "" {
			return []reconcile.Request{}
		}

		return []reconcile.Request{{
			NamespacedName: types.NamespacedName{
				Name: managedBy,
			},
		}}
	})
}
