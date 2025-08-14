package resources

import (
	"reflect"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
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

func Deleted() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return true
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
	}
}

// Content predicates moved from original controller.
var CMContentChangedPredicate = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		oldCM, _ := e.ObjectOld.(*corev1.ConfigMap)
		newCM, _ := e.ObjectNew.(*corev1.ConfigMap)
		return !reflect.DeepEqual(oldCM.Data, newCM.Data)
	},
}

var SecretContentChangedPredicate = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		oldSecret, _ := e.ObjectOld.(*corev1.Secret)
		newSecret, _ := e.ObjectNew.(*corev1.Secret)
		return !reflect.DeepEqual(oldSecret.Data, newSecret.Data)
	},
}

var DSCDeletionPredicate = predicate.Funcs{
	DeleteFunc: func(e event.DeleteEvent) bool {
		return true
	},
}

var DSCComponentUpdatePredicate = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		oldDSC, ok := e.ObjectOld.(*dscv1.DataScienceCluster)
		if !ok {
			return false
		}
		newDSC, ok := e.ObjectNew.(*dscv1.DataScienceCluster)
		if !ok {
			return false
		}
		// if .spec.components is changed, return true.
		if !reflect.DeepEqual(oldDSC.Spec.Components, newDSC.Spec.Components) {
			return true
		}

		// if new condition from component is added or removed, return true
		oldConditions := oldDSC.Status.Conditions
		newConditions := newDSC.Status.Conditions
		if len(oldConditions) != len(newConditions) {
			return true
		}

		// compare type one by one with their status if not equal return true
		for _, nc := range newConditions {
			for _, oc := range oldConditions {
				if nc.Type == oc.Type {
					if !reflect.DeepEqual(nc.Status, oc.Status) {
						return true
					}
				}
			}
		}
		return false
	},
}

var DSCIReadiness = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		oldObj, ok := e.ObjectOld.(*dsciv1.DSCInitialization)
		if !ok {
			return false
		}
		newObj, ok := e.ObjectNew.(*dsciv1.DSCInitialization)
		if !ok {
			return false
		}

		return oldObj.Status.Phase != newObj.Status.Phase
	},
	CreateFunc: func(e event.CreateEvent) bool {
		return false
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		return false
	},
	GenericFunc: func(e event.GenericEvent) bool {
		return false
	},
}

func AnnotationChanged(name string) predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return true
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return resources.GetAnnotation(e.ObjectNew, name) != resources.GetAnnotation(e.ObjectOld, name)
		},
	}
}

func CreatedOrUpdatedName(name string) predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.TypedCreateEvent[client.Object]) bool {
			return e.Object.GetName() == name
		},
		UpdateFunc: func(e event.TypedUpdateEvent[client.Object]) bool {
			return e.ObjectNew.GetName() == name
		},
	}
}

func CreatedOrUpdatedOrDeletedNamed(name string) predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.TypedCreateEvent[client.Object]) bool {
			return e.Object.GetName() == name
		},
		UpdateFunc: func(e event.TypedUpdateEvent[client.Object]) bool { return e.ObjectNew.GetName() == name },
		DeleteFunc: func(e event.TypedDeleteEvent[client.Object]) bool { return e.Object.GetName() == name },
	}
}

func CreatedOrUpdatedOrDeletedNamePrefixed(namePrefix string) predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.TypedCreateEvent[client.Object]) bool {
			return strings.HasPrefix(e.Object.GetName(), namePrefix)
		},
		UpdateFunc: func(e event.TypedUpdateEvent[client.Object]) bool {
			return strings.HasPrefix(e.ObjectNew.GetName(), namePrefix)
		},
		DeleteFunc: func(e event.TypedDeleteEvent[client.Object]) bool {
			return strings.HasPrefix(e.Object.GetName(), namePrefix)
		},
	}
}

var DSCIServiceMeshCondition = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		oldObj, ok := e.ObjectOld.(*dsciv1.DSCInitialization)
		if !ok {
			return false
		}
		newObj, ok := e.ObjectNew.(*dsciv1.DSCInitialization)
		if !ok {
			return false
		}

		oldMeshCondition := conditions.FindStatusCondition(&oldObj.Status, status.CapabilityServiceMesh)
		newMeshCondition := conditions.FindStatusCondition(&newObj.Status, status.CapabilityServiceMesh)

		if oldMeshCondition == nil || newMeshCondition == nil {
			return oldMeshCondition != newMeshCondition
		}

		return oldMeshCondition.Status != newMeshCondition.Status
	},
	CreateFunc: func(e event.CreateEvent) bool {
		return false
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		return false
	},
	GenericFunc: func(e event.GenericEvent) bool {
		return false
	},
}

var ServiceMeshStatusCondition = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		oldObj, ok := e.ObjectOld.(common.PlatformObject)
		if !ok {
			return false
		}
		newObj, ok := e.ObjectNew.(common.PlatformObject)
		if !ok {
			return false
		}

		oldMeshCondition := conditions.FindStatusCondition(oldObj.GetStatus(), status.CapabilityServiceMesh)
		newMeshCondition := conditions.FindStatusCondition(newObj.GetStatus(), status.CapabilityServiceMesh)
		oldAuthCondition := conditions.FindStatusCondition(oldObj.GetStatus(), status.CapabilityServiceMeshAuthorization)
		newAuthCondition := conditions.FindStatusCondition(newObj.GetStatus(), status.CapabilityServiceMeshAuthorization)

		meshConditionChanged := false
		authConditionChanged := false

		if oldMeshCondition == nil || newMeshCondition == nil {
			meshConditionChanged = oldMeshCondition != newMeshCondition
		} else {
			meshConditionChanged = oldMeshCondition.Status != newMeshCondition.Status
		}

		if oldAuthCondition == nil || newAuthCondition == nil {
			authConditionChanged = oldAuthCondition != newAuthCondition
		} else {
			authConditionChanged = oldAuthCondition.Status != newAuthCondition.Status
		}

		return meshConditionChanged || authConditionChanged
	},
	CreateFunc: func(e event.CreateEvent) bool {
		return false
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		return false
	},
	GenericFunc: func(e event.GenericEvent) bool {
		return false
	},
}
