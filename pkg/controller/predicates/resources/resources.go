package resources

import (
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var _ predicate.Predicate = DeploymentPredicate{}

type DeploymentPredicate struct {
	predicate.Funcs
}

// Update implements default UpdateEvent filter for validating generation change.
func (DeploymentPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return false
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
}

func NewDeploymentPredicate() *DeploymentPredicate {
	return &DeploymentPredicate{}
}

func NamespacedNamed(nn types.NamespacedName) predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return e.Object.GetName() == nn.Name && e.Object.GetNamespace() == nn.Namespace
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectNew.GetName() == nn.Name && e.ObjectNew.GetNamespace() == nn.Namespace
		},
	}
}
