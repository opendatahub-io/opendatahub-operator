package resources

import (
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

func NewDeploymentPredicate() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return true
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return true
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return true
		},

		UpdateFunc: func(e event.UpdateEvent) bool {
			if e.ObjectOld == nil || e.ObjectNew == nil {
				return true
			}

			oldDeployment, ok := e.ObjectOld.(*appsv1.Deployment)
			if !ok {
				return false
			}

			newDeployment, ok := e.ObjectNew.(*appsv1.Deployment)
			if !ok {
				return false
			}

			return oldDeployment.Generation != newDeployment.Generation ||
				oldDeployment.Status.Replicas != newDeployment.Status.Replicas ||
				oldDeployment.Status.ReadyReplicas != newDeployment.Status.ReadyReplicas
		},
	}
}
