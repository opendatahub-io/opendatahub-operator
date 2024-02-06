// Package certconfigmapgenerator contains generator logic of add cert configmap resource in user namespaces
package certconfigmapgenerator

import (
	"context"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/trustedcabundle"
)

var configmapGenLog = log.Log.WithName("cert-configmap-generator")

// CertConfigmapGeneratorReconciler holds the controller configuration.
type CertConfigmapGeneratorReconciler struct { //nolint:golint,revive // Readability
	Client client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// SetupWithManager sets up the controller with the Manager.
func (r *CertConfigmapGeneratorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	configmapGenLog.Info("Adding controller for Configmap Generation.")
	return ctrl.NewControllerManagedBy(mgr).
		Watches(&source.Kind{Type: &corev1.ConfigMap{}}, handler.EnqueueRequestsFromMapFunc(r.watchTrustedCABundleConfigMapResource), builder.WithPredicates(ConfigMapChangedPredicate)).
		Watches(&source.Kind{Type: &corev1.Namespace{}}, handler.EnqueueRequestsFromMapFunc(r.watchNamespaceResource), builder.WithPredicates(NamespaceCreatedPredicate)).
		Complete(r)
}

// Reconcile will generate new secret with random data for the annotated secret
// based on the specified type and complexity. This will avoid possible race
// conditions when a deployment mounts the secret before it is reconciled.
func (r *CertConfigmapGeneratorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Request includes namespace that is newly created or where odh-trusted-ca-bundle configmap is updated.
	r.Log.Info("Reconciling certConfigMapGenerator.", " CertConfigMapGenerator Request.Name", req.Name)
	// Get namespace instance
	userNamespace := &corev1.Namespace{}
	err := r.Client.Get(ctx, client.ObjectKey{Name: req.Namespace}, userNamespace)
	if err != nil {
		return ctrl.Result{}, errors.WithMessage(err, "error getting user namespace to inject trustedCA bundle ")
	}

	// Get DSCI instance
	dsciInstances := &dsci.DSCInitializationList{}
	err = r.Client.List(ctx, dsciInstances)
	if err != nil {
		r.Log.Error(err, "Failed to retrieve DSCInitialization resource for certconfigmapgenerator ", "CertConfigmapGenerator Request.Name", req.Name)
		return ctrl.Result{}, err
	}

	var dsciInstance *dsci.DSCInitialization
	switch len(dsciInstances.Items) {
	case 1:
		dsciInstance = &dsciInstances.Items[0]
	default:
		message := "only one instance of DSCInitialization object is allowed"
		return ctrl.Result{}, errors.New(message)
	}

	// Verify if namespace did not opt out of trustedCABundle injection
	if trustedcabundle.HasCABundleAnnotationDisabled(userNamespace) {
		err := trustedcabundle.DeleteOdhTrustedCABundleConfigMap(ctx, r.Client, req.Name)
		if err != nil {
			r.Log.Error(err, "error deleting existing configmap from namespace", "name", trustedcabundle.CAConfigMapName, "namespace", req.Name)
			return reconcile.Result{}, err
		}
	}

	// Verify odh-trusted-ca-bundle Configmap is created for the new namespace
	if trustedcabundle.ShouldInjectTrustedBundle(userNamespace) {
		err = trustedcabundle.CreateOdhTrustedCABundleConfigMap(ctx, r.Client, req.Name, dsciInstance)
		if err != nil {
			r.Log.Error(err, "error adding configmap to namespace", "name", trustedcabundle.CAConfigMapName, "namespace", req.Name)
			return reconcile.Result{}, err
		}
	}
	return ctrl.Result{}, err
}

func (r *CertConfigmapGeneratorReconciler) watchNamespaceResource(a client.Object) []reconcile.Request {
	if trustedcabundle.ShouldInjectTrustedBundle(a) {
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: a.GetNamespace()}}}
	}
	return nil
}

func (r *CertConfigmapGeneratorReconciler) watchTrustedCABundleConfigMapResource(a client.Object) []reconcile.Request {
	if a.GetName() == trustedcabundle.CAConfigMapName {
		return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: a.GetName(), Namespace: a.GetNamespace()}}}
	}
	return nil
}

var NamespaceCreatedPredicate = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool {
		return !trustedcabundle.HasCABundleAnnotationDisabled(e.Object)
	},

	// If user changes the annotation of namespace to opt out of CABundle injection, reconcile.
	UpdateFunc: func(updateEvent event.UpdateEvent) bool {
		return trustedcabundle.HasCABundleAnnotationDisabled(updateEvent.ObjectNew)
	},
}

var ConfigMapChangedPredicate = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		oldCM, _ := e.ObjectOld.(*corev1.ConfigMap)
		newCM, _ := e.ObjectNew.(*corev1.ConfigMap)

		return !reflect.DeepEqual(oldCM.Data, newCM.Data)
	},

	DeleteFunc: func(deleteEvent event.DeleteEvent) bool {
		return true
	},
}
