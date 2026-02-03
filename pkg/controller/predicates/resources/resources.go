package resources

import (
	"math"
	"reflect"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

var _ predicate.Predicate = DeploymentPredicate{}

// hasGVK checks if an unstructured object matches the expected GroupVersionKind.
func hasGVK(u *unstructured.Unstructured, expected schema.GroupVersionKind) bool {
	objGVK := u.GetObjectKind().GroupVersionKind()
	return objGVK == expected
}

type DeploymentPredicate struct {
	predicate.Funcs
}

// Update implements default UpdateEvent filter for validating generation change.
// Works with both typed *appsv1.Deployment and *unstructured.Unstructured objects.
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

// getDeploymentStatus extracts replicas and readyReplicas from a Deployment object.
// Supports both typed *appsv1.Deployment and *unstructured.Unstructured.
func getDeploymentStatus(obj client.Object) (int32, int32, bool) {
	// Try typed Deployment first
	if deploy, isTyped := obj.(*appsv1.Deployment); isTyped {
		return deploy.Status.Replicas, deploy.Status.ReadyReplicas, true
	}

	u, ok := obj.(*unstructured.Unstructured)
	if !ok || !hasGVK(u, gvk.Deployment) {
		return 0, 0, false
	}

	status, found, err := unstructured.NestedMap(u.Object, "status")
	if err != nil || !found {
		return 0, 0, true // No status yet, but valid object
	}

	return getInt32Field(status, "replicas"), getInt32Field(status, "readyReplicas"), true
}

func getInt32Field(m map[string]interface{}, field string) int32 {
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
	if !ok || !hasGVK(u, gvk.ConfigMap) {
		return nil, false
	}

	data, _, err := unstructured.NestedFieldNoCopy(u.Object, "data")
	if err != nil {
		return nil, false
	}
	return data, true
}

// Content predicates moved from original controller.
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
	if !ok || !hasGVK(u, gvk.Secret) {
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

// FIXME: is this function correct? By default CreateFunc and UpdateFunc are true.
var DSCDeletionPredicate = predicate.Funcs{
	DeleteFunc: func(e event.DeleteEvent) bool {
		return true
	},
}

func getDSC(obj client.Object) (*dscv2.DataScienceCluster, bool) {
	if dsc, ok := obj.(*dscv2.DataScienceCluster); ok {
		return dsc, true
	}

	u, err := resources.ToUnstructured(obj)
	if err != nil {
		return nil, false
	}
	dsc := &dscv2.DataScienceCluster{}
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, dsc)
	if err != nil {
		return nil, false
	}
	return dsc, true
}

var DSCComponentUpdatePredicate = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		oldDSC, ok := getDSC(e.ObjectOld)
		if !ok {
			return false
		}
		newDSC, ok := getDSC(e.ObjectNew)
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

// GatewayCertificateSecret returns a predicate that filters events for certificate secrets used by GatewayConfig.
// This includes both OpenShift default ingress certificates and user-provided certificates.
func GatewayCertificateSecret(isGatewayCertFn func(obj client.Object) bool) predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.TypedCreateEvent[client.Object]) bool {
			return isGatewayCertFn(e.Object)
		},
		UpdateFunc: func(e event.TypedUpdateEvent[client.Object]) bool {
			return isGatewayCertFn(e.ObjectNew)
		},
		// If certificate secret is deleted, trigger reconciliation to update status to error and warn user
		DeleteFunc: func(e event.TypedDeleteEvent[client.Object]) bool {
			return isGatewayCertFn(e.Object)
		},
	}
}

// GatewayStatusChanged returns a predicate that watches for Gateway status changes.
// This is used to trigger GatewayConfig reconciliation when the underlying Gateway
// becomes Accepted/Programmed, ensuring GatewayConfig status stays in sync with Gateway readiness.
func GatewayStatusChanged() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.TypedCreateEvent[client.Object]) bool {
			return false
		},
		UpdateFunc: func(e event.TypedUpdateEvent[client.Object]) bool {
			// Only trigger on status changes, not spec changes
			// Status changes have different resourceVersion but same generation
			return e.ObjectOld.GetResourceVersion() != e.ObjectNew.GetResourceVersion() &&
				e.ObjectOld.GetGeneration() == e.ObjectNew.GetGeneration()
		},
		DeleteFunc: func(e event.TypedDeleteEvent[client.Object]) bool {
			// Trigger reconciliation when Gateway is deleted to update GatewayConfig status
			return true
		},
	}
}

// HTTPRouteReferencesGateway returns a predicate that filters HTTPRoutes referencing the specified gateway.
func HTTPRouteReferencesGateway(gatewayName, gatewayNamespace string) predicate.Predicate {
	getHTTPRoute := func(obj client.Object) (*gwapiv1.HTTPRoute, bool) {
		httpRoute, ok := obj.(*gwapiv1.HTTPRoute)
		if ok {
			return httpRoute, true
		}
		u, err := resources.ToUnstructured(obj)
		if err != nil {
			return nil, false
		}
		httpRoute = &gwapiv1.HTTPRoute{}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, httpRoute)
		if err != nil {
			return nil, false
		}
		return httpRoute, true
	}

	referencesGateway := func(obj client.Object) bool {
		httpRoute, ok := getHTTPRoute(obj)
		if !ok {
			return false
		}
		for _, ref := range httpRoute.Spec.ParentRefs {
			refNamespace := gatewayNamespace
			if ref.Namespace != nil {
				refNamespace = string(*ref.Namespace)
			}
			if string(ref.Name) == gatewayName && refNamespace == gatewayNamespace {
				return true
			}
		}
		return false
	}

	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return referencesGateway(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return referencesGateway(e.ObjectNew)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return referencesGateway(e.Object)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return referencesGateway(e.Object)
		},
	}
}
