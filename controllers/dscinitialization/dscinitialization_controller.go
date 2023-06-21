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
	"github.com/go-logr/logr"

	addonv1alpha1 "github.com/openshift/addon-operator/apis/addons/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	authv1 "k8s.io/api/rbac/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	dsci "github.com/opendatahub-io/opendatahub-operator/apis/dscinitialization/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/pkg/deploy"
)

const (
	managedServiceApplicationNamespace = "redhat-ods-applications"
	defaultManifestPath                = "/opt/odh-manifests"
)

// DSCInitializationReconciler reconciles a DSCInitialization object
type DSCInitializationReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

//+kubebuilder:rbac:groups=*,resources=*,verbs=*
//+kubebuilder:rbac:groups=dscinitialization.opendatahub.io,resources=dscinitializations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=dscinitialization.opendatahub.io,resources=dscinitializations/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=dscinitialization.opendatahub.io,resources=dscinitializations/finalizers,verbs=update
//+kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups="",resources=services;namespaces;serviceaccounts;secrets;configmaps,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups=addons.managed.openshift.io,resources=addons,verbs=get;list
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings;roles;clusterrolebindings;clusterroles,verbs=get;list;watch;create;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the DSCInitialization object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *DSCInitializationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("Reconciling DSCInitialization.", "DSCInitialization", req.Namespace, "Request.Name", req.Name)

	instance := &dsci.DSCInitialization{}
	err := r.Client.Get(ctx, req.NamespacedName, instance)
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

	if r.isManagedService() {
		//Apply osd specific permissions
		err = deploy.DeployManifestsFromPath(instance, r.Client,
			defaultManifestPath+"/osd-configs",
			managedServiceApplicationNamespace, r.Scheme)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	// If monitoring enabled
	if instance.Spec.Monitoring.Enabled {
		if r.isManagedService() {
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
		For(&dsci.DSCInitialization{}).
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

func (r *DSCInitializationReconciler) isManagedService() bool {
	addonCRD := &apiextv1.CustomResourceDefinition{}

	err := r.Client.Get(context.TODO(), client.ObjectKey{Name: "addons.managed.openshift.io"}, addonCRD)
	if err != nil {
		if apierrs.IsNotFound(err) {
			return false
		} else {
			r.Log.Info("error getting Addon CRD for managed service", "err", err.Error())
			return false
		}
	} else {

		expectedAddon := &addonv1alpha1.Addon{}
		err := r.Client.Get(context.TODO(), client.ObjectKey{Name: "managed-odh"}, expectedAddon)
		if err != nil {
			if apierrs.IsNotFound(err) {
				return false
			} else {
				r.Log.Info("error getting Addon instance for managed service", "err", err.Error())
				return false
			}
		}
		return true
	}
}
