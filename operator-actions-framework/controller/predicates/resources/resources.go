package resources

import (
	"math"
	"reflect"
	"strings"

	"github.com/opendatahub-io/operator-actions-framework/resources"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var (
	gvkDeployment = schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"}
	gvkConfigMap  = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"}
	gvkSecret     = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Secret"}
)

var _ predicate.Predicate = DeploymentPredicate{}

func hasGVK(u *unstructured.Unstructured, expected schema.GroupVersionKind) bool {
	objGVK := u.GetObjectKind().GroupVersionKind()
	return objGVK == expected
}

type DeploymentPredicate struct {
	predicate.Funcs
}

func (DeploymentPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return false
	}

	oldReplicas, oldReadyReplicas, oldOk := getDeploymentStatus(e.ObjectOld)
	newReplicas, newReadyReplicas, newOk := getDeploymentStatus(e.ObjectNew)

	if !oldOk || !newOk {
		return false
	}

	return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() ||
		oldReplicas != newReplicas ||
		oldReadyReplicas != newReadyReplicas
}

func getDeploymentStatus(obj client.Object) (int32, int32, bool) {
	if deploy, isTyped := obj.(*appsv1.Deployment); isTyped {
		return deploy.Status.Replicas, deploy.Status.ReadyReplicas, true
	}

	u, ok := obj.(*unstructured.Unstructured)
	if !ok || !hasGVK(u, gvkDeployment) {
		return 0, 0, false
	}

	status, found, err := unstructured.NestedMap(u.Object, "status")
	if err != nil || !found {
		return 0, 0, true
	}

	return getInt32Field(status, "replicas"), getInt32Field(status, "readyReplicas"), true
}

func getInt32Field(m map[string]any, field string) int32 {
	val, found, err := unstructured.NestedInt64(m, field)
	if err != nil || !found {
		return 0
	}

	if val < math.MinInt32 {
		return math.MinInt32
	}
	if val > math.MaxInt32 {
		return math.MaxInt32
	}

	return int32(val)
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

func getConfigMapData(obj client.Object) (any, bool) {
	cm, ok := obj.(*corev1.ConfigMap)
	if ok {
		return cm.Data, true
	}

	u, ok := obj.(*unstructured.Unstructured)
	if !ok || !hasGVK(u, gvkConfigMap) {
		return nil, false
	}

	data, _, err := unstructured.NestedFieldNoCopy(u.Object, "data")
	if err != nil {
		return nil, false
	}
	return data, true
}

var CMContentChangedPredicate = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		oldData, oldOk := getConfigMapData(e.ObjectOld)
		newData, newOk := getConfigMapData(e.ObjectNew)
		if !oldOk || !newOk {
			return false
		}
		return !reflect.DeepEqual(oldData, newData)
	},
}

func getSecretData(obj client.Object) (any, bool) {
	secret, ok := obj.(*corev1.Secret)
	if ok {
		return secret.Data, true
	}

	u, ok := obj.(*unstructured.Unstructured)
	if !ok || !hasGVK(u, gvkSecret) {
		return nil, false
	}

	data, _, err := unstructured.NestedFieldNoCopy(u.Object, "data")
	if err != nil {
		return nil, false
	}
	return data, true
}

var SecretContentChangedPredicate = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		oldData, oldOk := getSecretData(e.ObjectOld)
		newData, newOk := getSecretData(e.ObjectNew)
		if !oldOk || !newOk {
			return false
		}
		return !reflect.DeepEqual(oldData, newData)
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

func CreatedOrUpdatedOrDeletedNamedInNamespace(name, namespace string) predicate.Predicate {
	return predicate.And(
		CreatedOrUpdatedOrDeletedNamed(name),
		predicate.NewPredicateFuncs(func(obj client.Object) bool {
			return obj.GetNamespace() == namespace
		}),
	)
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
		GenericFunc: func(e event.TypedGenericEvent[client.Object]) bool {
			return false
		},
	}
}

func CreatedOrUpdatedOrDeletedNameSuffixed(nameSuffix string) predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.TypedCreateEvent[client.Object]) bool {
			return strings.HasSuffix(e.Object.GetName(), nameSuffix)
		},
		UpdateFunc: func(e event.TypedUpdateEvent[client.Object]) bool {
			return strings.HasSuffix(e.ObjectNew.GetName(), nameSuffix)
		},
		DeleteFunc: func(e event.TypedDeleteEvent[client.Object]) bool {
			return strings.HasSuffix(e.Object.GetName(), nameSuffix)
		},
	}
}

func GatewayCertificateSecret(isGatewayCertFn func(obj client.Object) bool) predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.TypedCreateEvent[client.Object]) bool {
			return isGatewayCertFn(e.Object)
		},
		UpdateFunc: func(e event.TypedUpdateEvent[client.Object]) bool {
			return isGatewayCertFn(e.ObjectNew)
		},
		DeleteFunc: func(e event.TypedDeleteEvent[client.Object]) bool {
			return isGatewayCertFn(e.Object)
		},
	}
}

func StatusChanged() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.TypedCreateEvent[client.Object]) bool {
			return false
		},
		UpdateFunc: func(e event.TypedUpdateEvent[client.Object]) bool {
			return e.ObjectOld.GetResourceVersion() != e.ObjectNew.GetResourceVersion() &&
				e.ObjectOld.GetGeneration() == e.ObjectNew.GetGeneration()
		},
		DeleteFunc: func(e event.TypedDeleteEvent[client.Object]) bool {
			return true
		},
	}
}

func CreatedOrUpdatedOrDeletedLabeled(key, value string) predicate.Predicate {
	hasLabel := func(obj client.Object) bool {
		return obj.GetLabels()[key] == value
	}
	return predicate.Funcs{
		CreateFunc:  func(e event.TypedCreateEvent[client.Object]) bool { return hasLabel(e.Object) },
		UpdateFunc:  func(e event.TypedUpdateEvent[client.Object]) bool { return hasLabel(e.ObjectNew) },
		DeleteFunc:  func(e event.TypedDeleteEvent[client.Object]) bool { return hasLabel(e.Object) },
		GenericFunc: func(e event.TypedGenericEvent[client.Object]) bool { return false },
	}
}
