/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package dscinitialization

import (
	"context"
	"errors"

	logr "github.com/go-logr/logr"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	authv1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"

	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"k8s.io/client-go/tools/record"
)

// DSCInitializationReconciler reconciles a DSCInitialization object
type DSCInitializationReconciler struct {
	client.Client
	Scheme                *runtime.Scheme
	Log                   logr.Logger
	Recorder              record.EventRecorder
	ApplicationsNamespace string
}

// +kubebuilder:rbac:groups=*,resources=*,verbs=*

// Reconcile contains controller logic specific to DSCInitialization instance updates
// +kubebuilder:rbac:groups=dscinitialization.opendatahub.io,resources=dscinitializations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=dscinitialization.opendatahub.io,resources=dscinitializations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=dscinitialization.opendatahub.io,resources=dscinitializations/finalizers,verbs=update
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="core",resources=services;namespaces;serviceaccounts;secrets;configmaps,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=addons.managed.openshift.io,resources=addons,verbs=get;list
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings;roles;clusterrolebindings;clusterroles,verbs=get;list;watch;create;update;patch

func (r *DSCInitializationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("Reconciling DSCInitialization.", "DSCInitialization", req.Namespace, "Request.Name", req.Name)

	instance := &dsci.DSCInitialization{}
	// Only apply reconcile logic to 'default' instance of DataScienceInitialization
	err := r.Client.Get(ctx, types.NamespacedName{Name: "default"}, instance)
	if err != nil {
		if apierrs.IsNotFound(err) {
			// DataScienceInitialization instance not found.
			return ctrl.Result{}, nil
		}
		r.Log.Error(err, "Failed to retrieve DSCInitialization resource.", "DSCInitialization", req.Namespace, "Request.Name", req.Name)
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError", "Failed to retrieve DSCInitialization instance")
		return ctrl.Result{}, err
	}

	// Start reconciling
	if instance.Status.Conditions == nil {
		reason := status.ReconcileInit
		message := "Initializing DSCInitialization resource"
		instance, err = r.updateStatus(instance, func(saved *dsci.DSCInitialization) {
			status.SetProgressingCondition(&saved.Status.Conditions, reason, message)
			saved.Status.Phase = status.PhaseProgressing
		})
		if err != nil {
			r.Log.Error(err, "Failed to add conditions to status of DSCInitialization resource.", "DSCInitialization", req.Namespace, "Request.Name", req.Name)
			r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError",
				"%s for instance %s", message, instance.Name)
			return reconcile.Result{}, err
		}
	}

	// Check namespace
	namespace := instance.Spec.ApplicationsNamespace
	err = r.createOdhNamespace(instance, namespace, ctx)
	if err != nil {
		// no need to log error as it was already logged in createOdhNamespace
		return reconcile.Result{}, err
	}

	// Extract latest Manifests
	err = deploy.DownloadManifests(instance.Spec.ManifestsUri)
	if err != nil {
		r.Log.Error(err, "Failed to download and unpack manifests.", "ManifestsURI", instance.Spec.ManifestsUri)
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError", "Failed to download and unpack manifests")
		return reconcile.Result{}, err
	}

	// Get platform
	platform, err := deploy.GetPlatform(r.Client)
	if err != nil {
		r.Log.Error(err, "Failed to determine platform (managed vs self-managed)")
		return reconcile.Result{}, err
	}

	// Apply update from legacy operator
	// TODO: Update upgrade logic to get components through KfDef
	//if err = updatefromLegacyVersion(r.Client); err != nil {
	//	r.Log.Error(err, "unable to update from legacy operator version")
	//	return reconcile.Result{}, err
	//}

	// Apply Rhods specific configs
	if platform == deploy.ManagedRhods || platform == deploy.SelfManagedRhods {
		//Apply osd specific permissions
		if platform == deploy.ManagedRhods {
			err = deploy.DeployManifestsFromPath(instance, r.Client, "osd",
				deploy.DefaultManifestPath+"/osd-configs",
				r.ApplicationsNamespace, r.Scheme, true)
			if err != nil {
				r.Log.Error(err, "Failed to apply osd specific configs from manifests", "Manifests path", deploy.DefaultManifestPath+"/osd-configs")
				r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError", "Failed to apply "+deploy.DefaultManifestPath+"/osd-configs")
				return reconcile.Result{}, err
			}
		}

		// Apply rhods-specific config
		// Create rhods-notebooks namespace
		err = r.createOdhNamespace(instance, "rhods-notebooks", ctx)
		if err != nil {
			// no need to log error as it was already logged in createOdhNamespace
			return reconcile.Result{}, err
		}
	}

	// If monitoring enabled
	if instance.Spec.Monitoring.Enabled {
		switch platform {
		case deploy.SelfManagedRhods:
			r.Log.Info("Monitoring enabled, won't apply changes", "cluster", "Self-Managed RHODS Mode")
			err := r.configureCommonMonitoring(instance)
			if err != nil {
				return reconcile.Result{}, err
			}
		case deploy.ManagedRhods:
			r.Log.Info("Monitoring enabled", "cluster", "Managed Service Mode")
			err := r.configureManagedMonitoring(instance)
			if err != nil {
				// no need to log error as it was already logged in configureManagedMonitoring
				return reconcile.Result{}, err
			}
			err = r.configureCommonMonitoring(instance)
			if err != nil {
				return reconcile.Result{}, err
			}
		default:
			// TODO: ODH specific monitoring logic
			r.Log.Info("Monitoring enabled, won't apply changes", "cluster", "ODH Mode")
		}
	}

	// Finish reconciling
	_, err = r.updateStatus(instance, func(saved *dsci.DSCInitialization) {
		status.SetCompleteCondition(&saved.Status.Conditions, status.ReconcileCompleted, status.ReconcileCompletedMessage)
		saved.Status.Phase = status.PhaseReady
	})
	if err != nil {
		r.Log.Error(err, "failed to update DSCInitialization status after successfuly completed reconciliation")
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError", "Failed to update DSCInitialization status")
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DSCInitializationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dsci.DSCInitialization{}, builder.WithPredicates(singletonPredicate)).
		Owns(&corev1.Namespace{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&netv1.NetworkPolicy{}).
		Owns(&authv1.Role{}).
		Owns(&authv1.RoleBinding{}).
		Owns(&authv1.ClusterRole{}).
		Owns(&authv1.ClusterRoleBinding{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.ReplicaSet{}).
		Owns(&corev1.Pod{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.Service{}).
		// this predicates prevents meaningless reconciliations from being triggered
		WithEventFilter(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{})).
		Complete(r)
}

var singletonPredicate = predicate.Funcs{
	// Only reconcile on 'default' initialization

	CreateFunc: func(e event.CreateEvent) bool {
		if e.Object.GetObjectKind().GroupVersionKind().Kind == "DSCInitialization" {
			if e.Object.GetName() == "default" {
				return true
			} else {
				// Set to error level since it causes Panic
				setupLog := ctrl.Log.WithName("dscinitialization")
				setupLog.Error(errors.New("only single DSCInitialization instance can be created. Mismatch CreateEvent Object.GetName not to 'default'"), "Wrong name", "object", e.Object.GetName())
				return false
			}
		}

		return true
	},

	UpdateFunc: func(e event.UpdateEvent) bool {
		// handle update events
		if e.ObjectNew.GetObjectKind().GroupVersionKind().Kind == "DSCInitialization" {
			if e.ObjectNew.GetName() == "default" {
				return true
			} else {
				setupLog := ctrl.Log.WithName("dscinitialization")
				setupLog.Error(errors.New("only single DSCInitialization instance can be updated. Mismatch UpdateEvent Object.GetName not to 'default'"), "Wrong name", "object", e.ObjectNew.GetName())
				return false
			}
		}
		return true
	},
}

func (r *DSCInitializationReconciler) updateStatus(original *dsci.DSCInitialization, update func(saved *dsci.DSCInitialization)) (*dsci.DSCInitialization, error) {
	saved := &dsci.DSCInitialization{}
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {

		err := r.Client.Get(context.TODO(), client.ObjectKeyFromObject(original), saved)
		if err != nil {
			return err
		}
		// update status here
		update(saved)

		// Try to update
		err = r.Client.Status().Update(context.TODO(), saved)
		// Return err itself here (not wrapped inside another error)
		// so that RetryOnConflict can identify it correctly.
		return err
	})
	return saved, err
}
