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

	dsci "github.com/opendatahub-io/opendatahub-operator/apis/dscinitialization/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/pkg/deploy"
)

// DSCInitializationReconciler reconciles a DSCInitialization object
type DSCInitializationReconciler struct {
	client.Client
	Scheme                *runtime.Scheme
	Log                   logr.Logger
	ApplicationsNamespace string
}

// +kubebuilder:rbac:groups=*,resources=*,verbs=*
<<<<<<< HEAD
=======

// Reconcile contains controller logic specific to DSCInitialization instance updates
>>>>>>> 9f4e5b2 (Update manager role)
// +kubebuilder:rbac:groups=dscinitialization.opendatahub.io,resources=dscinitializations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=dscinitialization.opendatahub.io,resources=dscinitializations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=dscinitialization.opendatahub.io,resources=dscinitializations/finalizers,verbs=update
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=services;namespaces;serviceaccounts;secrets;configmaps,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=addons.managed.openshift.io,resources=addons,verbs=get;list
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings;roles;clusterrolebindings;clusterroles,verbs=get;list;watch;create;update;patch

func (r *DSCInitializationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("Reconciling DSCInitialization.", "DSCInitialization", req.Namespace, "Request.Name", req.Name)

	instance := &dsci.DSCInitialization{}
	// Only apply reconcile logic to 'default' instance of DataScienceInitialization
	err := r.Client.Get(ctx, types.NamespacedName{Name: "default"}, instance)
	if err != nil && apierrs.IsNotFound(err) {
		return ctrl.Result{}, nil
	} else if err != nil {
		return ctrl.Result{}, err
	}

	// Start reconciling
	if instance.Status.Conditions == nil {
		reason := status.ReconcileInit
		message := "Initializing DSCInitialization resource"
		status.SetProgressingCondition(&instance.Status.Conditions, reason, message)

		instance.Status.Phase = status.PhaseProgressing
		err = r.Client.Status().Update(ctx, instance)
		if err != nil {
			r.Log.Error(err, "Failed to add conditions to status of DSCInitialization resource.", "DSCInitialization", req.Namespace, "Request.Name", req.Name)
			return reconcile.Result{}, err
		}
	}

	// Check for list of namespaces
	for _, namespace := range instance.Spec.Namespaces {
		err = r.createOdhNamespace(instance, namespace, ctx)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	// Extract latest Manifests
	err = deploy.DownloadManifests(instance.Spec.ManifestsUri)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Get platform
	platform, err := deploy.GetPlatform(r.Client)
	if err != nil {
		return reconcile.Result{}, err

	}

	// Apply Rhods specific configs
	if platform == deploy.ManagedRhods || platform == deploy.SelfManagedRhods {
		//Apply osd specific permissions
		err = deploy.DeployManifestsFromPath(instance, r.Client,
			deploy.DefaultManifestPath+"/osd-configs",
			r.ApplicationsNamespace, r.Scheme)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	// If monitoring enabled
	if instance.Spec.Monitoring.Enabled {
		if platform == deploy.ManagedRhods {
			err := r.configureManagedMonitoring(instance)
			if err != nil {
				return reconcile.Result{}, err
			}

		} else {
			// TODO: ODH specific monitoring logic
		}
	}

	// Finish reconciling
	reason := status.ReconcileCompleted
	message := status.ReconcileCompletedMessage
	status.SetCompleteCondition(&instance.Status.Conditions, reason, message)

	instance.Status.Phase = status.PhaseReady
	err = r.Client.Status().Update(ctx, instance)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DSCInitializationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dsci.DSCInitialization{}, builder.WithPredicates(singletonPredicate)).
		Owns(&corev1.Secret{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&netv1.NetworkPolicy{}).
		Owns(&authv1.Role{}).
		Owns(&authv1.RoleBinding{}).
		Owns(&authv1.ClusterRole{}).
		Owns(&authv1.ClusterRoleBinding{}).
		Owns(&corev1.Namespace{}).
		Owns(&corev1.Pod{}).
		Owns(&appsv1.Deployment{}).
		Owns(&appsv1.ReplicaSet{}).
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
			}
		}
		// Set to error level since it causes Panic
		setupLog := ctrl.Log.WithName("dscinitialization")
		setupLog.Error(errors.New("Only single DSCInitialization instance can be created. Mismatch CreateEvent Object.GetName not to 'default'"), "Wrong name", "object", e.Object.GetName())
		return false
	},

	UpdateFunc: func(e event.UpdateEvent) bool {
		// handle update events
		if e.ObjectNew.GetObjectKind().GroupVersionKind().Kind == "DSCInitialization" {
			if e.ObjectNew.GetName() == "default" {
				return true
			}
		}
		setupLog := ctrl.Log.WithName("dscinitialization")
		setupLog.Error(errors.New("Only single DSCInitialization instance can be updated. Mismatch UpdateEvent Object.GetName not to 'default'"), "Wrong name", "object", e.ObjectNew.GetName())
		return false
	},
}
