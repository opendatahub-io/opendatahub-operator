package resources

import (
	"fmt"
	"reflect"

	fwres "github.com/opendatahub-io/operator-actions-framework/controller/predicates/resources"
	configv1 "github.com/openshift/api/config/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

type DeploymentPredicate = fwres.DeploymentPredicate

var (
	NewDeploymentPredicate                    = fwres.NewDeploymentPredicate
	Deleted                                   = fwres.Deleted
	CMContentChangedPredicate                 = fwres.CMContentChangedPredicate
	SecretContentChangedPredicate             = fwres.SecretContentChangedPredicate
	AnnotationChanged                         = fwres.AnnotationChanged
	CreatedOrUpdatedName                      = fwres.CreatedOrUpdatedName
	CreatedOrUpdatedOrDeletedNamed            = fwres.CreatedOrUpdatedOrDeletedNamed
	CreatedOrUpdatedOrDeletedNamedInNamespace = fwres.CreatedOrUpdatedOrDeletedNamedInNamespace
	CreatedOrUpdatedOrDeletedNamePrefixed     = fwres.CreatedOrUpdatedOrDeletedNamePrefixed
	CreatedOrUpdatedOrDeletedNameSuffixed     = fwres.CreatedOrUpdatedOrDeletedNameSuffixed
	GatewayCertificateSecret                  = fwres.GatewayCertificateSecret
	CreatedOrUpdatedOrDeletedLabeled          = fwres.CreatedOrUpdatedOrDeletedLabeled
)

func hasGVK(u *unstructured.Unstructured, expected schema.GroupVersionKind) bool {
	objGVK := u.GetObjectKind().GroupVersionKind()
	return objGVK == expected
}

// GatewayStatusChanged returns a predicate that watches for Gateway status changes.
func GatewayStatusChanged() predicate.Predicate {
	return fwres.StatusChanged()
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
		if !reflect.DeepEqual(oldDSC.Spec.Components, newDSC.Spec.Components) {
			return true
		}

		oldConditions := oldDSC.Status.Conditions
		newConditions := newDSC.Status.Conditions
		if len(oldConditions) != len(newConditions) {
			return true
		}

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

func toGatewayConfig(obj client.Object) (*serviceApi.GatewayConfig, error) {
	if gc, ok := obj.(*serviceApi.GatewayConfig); ok {
		return gc, nil
	}

	if unstr, ok := obj.(*unstructured.Unstructured); ok {
		var gc serviceApi.GatewayConfig
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstr.Object, &gc); err != nil {
			return nil, fmt.Errorf("failed to convert unstructured to GatewayConfig: %w", err)
		}
		return &gc, nil
	}

	return nil, fmt.Errorf("object is neither typed GatewayConfig nor unstructured: %T", obj)
}

// GatewayConfigDomainChanged returns a predicate that triggers reconciliation only when GatewayConfig's
// status.Domain field changes.
func GatewayConfigDomainChanged() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return true
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return true
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldGC, err := toGatewayConfig(e.ObjectOld)
			if err != nil {
				return false
			}

			newGC, err := toGatewayConfig(e.ObjectNew)
			if err != nil {
				return false
			}

			return oldGC.Status.Domain != newGC.Status.Domain
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
	}
}

// APIServerTLSSecurityProfileChanged returns a predicate that triggers reconciliation when the
// cluster APIServer spec.tlsSecurityProfile changes.
func APIServerTLSSecurityProfileChanged() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return isClusterAPIServer(e.Object)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return isClusterAPIServer(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			if !isClusterAPIServer(e.ObjectNew) {
				return false
			}
			oldAPI, oldErr := toAPIServer(e.ObjectOld)
			newAPI, newErr := toAPIServer(e.ObjectNew)
			if oldErr != nil || newErr != nil {
				// Reconcile when either object cannot be converted so TLS args stay in sync.
				return true
			}
			return !reflect.DeepEqual(oldAPI.Spec.TLSSecurityProfile, newAPI.Spec.TLSSecurityProfile)
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
	}
}

func isClusterAPIServer(obj client.Object) bool {
	if obj == nil {
		return false
	}
	return obj.GetName() == cluster.ClusterAPIServerObj
}

func toAPIServer(obj client.Object) (*configv1.APIServer, error) {
	if apiServer, ok := obj.(*configv1.APIServer); ok {
		return apiServer, nil
	}
	if unstr, ok := obj.(*unstructured.Unstructured); ok {
		if !hasGVK(unstr, gvk.OpenshiftAPIServer) {
			return nil, fmt.Errorf("unexpected GVK: %v", unstr.GetObjectKind().GroupVersionKind())
		}
		var apiServer configv1.APIServer
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstr.Object, &apiServer); err != nil {
			return nil, fmt.Errorf("failed to convert unstructured to APIServer: %w", err)
		}
		return &apiServer, nil
	}
	return nil, fmt.Errorf("object is neither typed APIServer nor unstructured: %T", obj)
}
