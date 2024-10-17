// Package certconfigmapgenerator contains generator logic of add cert configmap resource in user namespaces
package certconfigmapgenerator

import (
	"context"
	"reflect"

	"github.com/go-logr/logr"
	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	annotation "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/trustedcabundle"
)

// CertConfigmapGeneratorReconciler holds the controller configuration.
type CertConfigmapGeneratorReconciler struct {
	Client client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// SetupWithManager sets up the controller with the Manager.
func (r *CertConfigmapGeneratorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Log.Info("Adding controller for Configmap Generation.")
	return ctrl.NewControllerManagedBy(mgr).
		Named("cert-configmap-generator-controller").
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(r.watchTrustedCABundleConfigMapResource), builder.WithPredicates(ConfigMapChangedPredicate)).
		Watches(&corev1.Namespace{}, handler.EnqueueRequestsFromMapFunc(r.watchNamespaceResource), builder.WithPredicates(NamespaceCreatedPredicate)).
		Complete(r)
}

// Reconcile will generate new configmap, odh-trusted-ca-bundle, that includes cluster-wide trusted-ca bundle and custom
// ca bundle in every new namespace created.
func (r *CertConfigmapGeneratorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Request includes namespace that is newly created or where odh-trusted-ca-bundle configmap is updated.
	r.Log.Info("Reconciling CertConfigMapGenerator.", " Request.Namespace", req.NamespacedName)
	// Get namespace instance
	userNamespace := &corev1.Namespace{}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: req.Namespace}, userNamespace); err != nil {
		return ctrl.Result{}, errors.WithMessage(err, "error getting namespace to inject trustedCA bundle")
	}

	// Get DSCI instance
	dsciInstances := &dsciv1.DSCInitializationList{}
	if err := r.Client.List(ctx, dsciInstances); err != nil {
		r.Log.Error(err, "Failed to retrieve DSCInitialization resource for CertConfigMapGenerator ", "Request.Name", req.Name)
		return ctrl.Result{}, err
	}

	var dsciInstance *dsciv1.DSCInitialization
	switch len(dsciInstances.Items) {
	case 0:
		return ctrl.Result{}, nil
	case 1:
		dsciInstance = &dsciInstances.Items[0]
	}

	if skipApplyTrustCAConfig(dsciInstance.Spec.TrustedCABundle) {
		return ctrl.Result{}, nil
	}

	// Delete odh-trusted-ca-bundle Configmap if namespace has annotation set to opt-out CA bundle injection
	if trustedcabundle.HasCABundleAnnotationDisabled(userNamespace) {
		r.Log.Info("Namespace has opted-out of CA bundle injection using annotation", "namespace", userNamespace.Name,
			"annotation", annotation.InjectionOfCABundleAnnotatoion)
		if err := trustedcabundle.DeleteOdhTrustedCABundleConfigMap(ctx, r.Client, req.Namespace); client.IgnoreNotFound(err) != nil {
			r.Log.Error(err, "error deleting existing configmap from namespace", "name", trustedcabundle.CAConfigMapName, "namespace", userNamespace.Name)
			return reconcile.Result{}, err
		}

		return reconcile.Result{}, nil
	}

	// Add odh-trusted-ca-bundle Configmap
	if trustedcabundle.ShouldInjectTrustedBundle(userNamespace) {
		r.Log.Info("Adding trusted CA bundle configmap to the new or existing namespace ", "namespace", userNamespace.Name,
			"configmap", trustedcabundle.CAConfigMapName)
		trustCAData := dsciInstance.Spec.TrustedCABundle.CustomCABundle
		if err := trustedcabundle.CreateOdhTrustedCABundleConfigMap(ctx, r.Client, req.Namespace, trustCAData); err != nil {
			r.Log.Error(err, "error adding configmap to namespace", "name", trustedcabundle.CAConfigMapName, "namespace", userNamespace.Name)
			return reconcile.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *CertConfigmapGeneratorReconciler) watchNamespaceResource(_ context.Context, a client.Object) []reconcile.Request {
	namespace, isNamespaceObject := a.(*corev1.Namespace)
	if !isNamespaceObject {
		return nil
	}
	if trustedcabundle.ShouldInjectTrustedBundle(namespace) {
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: trustedcabundle.CAConfigMapName, Namespace: a.GetName()}}}
	}
	return nil
}

func (r *CertConfigmapGeneratorReconciler) watchTrustedCABundleConfigMapResource(_ context.Context, a client.Object) []reconcile.Request {
	if a.GetName() == trustedcabundle.CAConfigMapName {
		r.Log.Info("Cert configmap has been updated, start reconcile")
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: a.GetName(), Namespace: a.GetNamespace()}}}
	}
	return nil
}

var NamespaceCreatedPredicate = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool {
		namespace, isNamespaceObject := e.Object.(*corev1.Namespace)
		if !isNamespaceObject {
			return false
		}
		return trustedcabundle.ShouldInjectTrustedBundle(namespace)
	},

	// If user changes the annotation of namespace to opt out of CABundle injection, reconcile.
	UpdateFunc: func(e event.UpdateEvent) bool {
		oldNamespace, _ := e.ObjectOld.(*corev1.Namespace)
		newNamespace, _ := e.ObjectNew.(*corev1.Namespace)

		oldNsAnnValue, oldNsAnnExists := oldNamespace.GetAnnotations()[annotation.InjectionOfCABundleAnnotatoion]
		newNsAnnValue, newNsAnnExists := newNamespace.GetAnnotations()[annotation.InjectionOfCABundleAnnotatoion]

		if newNsAnnExists && !oldNsAnnExists {
			return true
		} else if newNsAnnExists && oldNsAnnExists && oldNsAnnValue != newNsAnnValue {
			return true
		}
		return false
	},

	DeleteFunc: func(deleteEvent event.DeleteEvent) bool {
		return false
	},
}

var ConfigMapChangedPredicate = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		oldCM, _ := e.ObjectOld.(*corev1.ConfigMap)
		newCM, _ := e.ObjectNew.(*corev1.ConfigMap)
		return !reflect.DeepEqual(oldCM.Data, newCM.Data)
	},
}

func skipApplyTrustCAConfig(dsciConfigTrustCA *dsciv1.TrustedCABundleSpec) bool {
	return dsciConfigTrustCA == nil || dsciConfigTrustCA.ManagementState != operatorv1.Managed
}
