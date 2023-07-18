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

package datasciencecluster

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	dsc "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/datasciencepipelines"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/kserve"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/modelmeshserving"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/workbenches"

	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	corev1 "k8s.io/api/core/v1"
	authv1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DataScienceClusterReconciler reconciles a DataScienceCluster object
type DataScienceClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
	// Recorder to generate events
	Recorder              record.EventRecorder
	ApplicationsNamespace string
}

//+kubebuilder:rbac:groups=datasciencecluster.opendatahub.io,resources=datascienceclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=datasciencecluster.opendatahub.io,resources=datascienceclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=datasciencecluster.opendatahub.io,resources=datascienceclusters/finalizers,verbs=update
//+kubebuilder:rbac:groups=addons.managed.openshift.io,resources=addons,verbs=get;list
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings;roles;clusterrolebindings;clusterroles,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups=apps,resources=deployments;daemonsets;replicasets;statefulsets,verbs=*
//+kubebuilder:rbac:groups="",resources=pods;services;services/finalizers;endpoints;persistentvolumeclaims;events;configmaps;secrets,verbs="*"

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *DataScienceClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("Reconciling DataScienceCluster resources", "Request.Namespace", req.Namespace, "Request.Name", req.Name)

	instance := &dsc.DataScienceCluster{}
	err := r.Client.Get(context.TODO(), req.NamespacedName, instance)
	if err != nil && apierrs.IsNotFound(err) {
		return ctrl.Result{}, nil
	} else if err != nil {
		return ctrl.Result{}, err
	}

<<<<<<< HEAD
	// If instance is being deleted, return
	if instance.GetDeletionTimestamp() != nil {
		return ctrl.Result{}, nil
	}

	// Return if multiple instances of DataScienceCluster exist
	instanceList := &dsc.DataScienceClusterList{}
	err = r.Client.List(context.TODO(), instanceList)
	if err != nil {
		return ctrl.Result{}, err
	}

=======
	var instance *dsc.DataScienceCluster
>>>>>>> 16be06a (Adding retry on failure capability)
	if len(instanceList.Items) > 1 {
		message := fmt.Sprintf("only one instance of DataScienceCluster object is allowed. Update existing instance on namespace %s and name %s", req.Namespace, req.Name)
		_ = r.reportError(err, &instanceList.Items[0], message)
		return ctrl.Result{}, fmt.Errorf(message)
<<<<<<< HEAD
=======
	} else if len(instanceList.Items) != 0 {
		instance = &instanceList.Items[0]
	} else {
		// this should never happen
		return ctrl.Result{}, nil
	}

	// If instance is being deleted, return
	if instance.GetDeletionTimestamp() != nil {
		return ctrl.Result{}, err
>>>>>>> 16be06a (Adding retry on failure capability)
	}

	// Start reconciling
	if instance.Status.Conditions == nil {
		reason := status.ReconcileInit
		message := "Initializing DataScienceCluster resource"
		status.SetProgressingCondition(&instance.Status.Conditions, reason, message)
		instance.Status.Phase = status.PhaseProgressing
		instance, err = r.updateStatus(instance)
		if err != nil {
			_ = r.reportError(err, instance, fmt.Sprintf("failed to add conditions to status of DataScienceCluster resource on namespace %s and name %s", req.Namespace, req.Name))
			return ctrl.Result{}, err
		}
	}
	if instance.Status.InstalledComponents == nil {
		instance.Status.InstalledComponents = make(map[string]bool)
	}

	// reconcile dashboard component
	var val ctrl.Result
	if instance, val, err = r.reconcileSubComponent(instance, dashboard.ComponentName, instance.Spec.Components.Dashboard.Enabled,
		&(instance.Spec.Components.Dashboard), ctx); err != nil {
		// no need to log any errors as this is done in the reconcileSubComponent method
		return val, err
	}

	// reconcile DataSciencePipelines component
	if instance, val, err = r.reconcileSubComponent(instance, datasciencepipelines.ComponentName, instance.Spec.Components.DataSciencePipelines.Enabled,
		&(instance.Spec.Components.DataSciencePipelines), ctx); err != nil {
		// no need to log any errors as this is done in the reconcileSubComponent method
		return val, err
	}

	// reconcile ModelMesh component
	if instance, val, err = r.reconcileSubComponent(instance, modelmeshserving.ComponentName, instance.Spec.Components.ModelMeshServing.Enabled,
		&(instance.Spec.Components.ModelMeshServing), ctx); err != nil {
		// no need to log any errors as this is done in the reconcileSubComponent method
		return val, err
	}

	// reconcile Workbench component
	if instance, val, err = r.reconcileSubComponent(instance, workbenches.ComponentName, instance.Spec.Components.Workbenches.Enabled,
		&(instance.Spec.Components.Workbenches), ctx); err != nil {
		// no need to log any errors as this is done in the reconcileSubComponent method
		return val, err
	}

	// reconcile Kserve component
	if instance, val, err = r.reconcileSubComponent(instance, kserve.ComponentName, instance.Spec.Components.Kserve.Enabled, &(instance.Spec.Components.Kserve), ctx); err != nil {
		// no need to log any errors as this is done in the reconcileSubComponent method
		return val, err
	}

	// finalize reconciliation
	status.SetCompleteCondition(&instance.Status.Conditions, status.ReconcileCompleted, "DataScienceCluster resource reconciled successfully.")
	instance.Status.Phase = status.PhaseReady
	instance, err = r.updateStatus(instance)
	if err != nil {
		r.Log.Error(err, "failed to update DataScienceCluster conditions after successfuly completed reconciliation")
		return ctrl.Result{}, err
	} else {
		r.Log.Info("DataScienceCluster Deployment Completed.")
		r.Recorder.Eventf(instance, corev1.EventTypeNormal, "DataScienceClusterCreationSuccessful",
			"DataScienceCluster instance %s created and deployed successfully", instance.Name)
	}
	return ctrl.Result{}, nil
}

func (r *DataScienceClusterReconciler) reconcileSubComponent(instance *dsc.DataScienceCluster, componentName string, enabled bool,
	component components.ComponentInterface, ctx context.Context) (*dsc.DataScienceCluster, ctrl.Result, error) {
	err := component.ReconcileComponent(instance, r.Client, r.Scheme, enabled, r.ApplicationsNamespace)
	if err != nil {
		instance = r.reportError(err, instance, "failed to reconcile "+componentName+" on DataScienceCluster")
		return instance, ctrl.Result{}, err
	}
	instance.Status.InstalledComponents[componentName] = enabled

	if instance, err = r.updateStatus(instance); err != nil {
		instance = r.reportError(err, instance, "failed to update DataScienceCluster status after reconciling "+componentName)
		return instance, ctrl.Result{}, err
	}
	return instance, ctrl.Result{}, nil
}

func (r *DataScienceClusterReconciler) reportError(err error, instance *dsc.DataScienceCluster, message string) *dsc.DataScienceCluster {
	r.Log.Error(err, message, "instance.Name", instance.Name)
	r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DataScienceClusterReconcileError",
		"%s for instance %s", message, instance.Name)
	status.SetErrorCondition(&instance.Status.Conditions, status.ReconcileFailed, fmt.Sprintf("%s : %v", message, err))
	instance.Status.Phase = status.PhaseError
	if instance, err = r.updateStatus(instance); err != nil {
		r.Log.Error(err, "failed to update DataScienceCluster status after error", "instance.Name", instance.Name)
	}
	return instance
}

// SetupWithManager sets up the controller with the Manager.
func (r *DataScienceClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dsc.DataScienceCluster{}).
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

func (r *DataScienceClusterReconciler) updateStatus(original *dsc.DataScienceCluster) (*dsc.DataScienceCluster, error) {
	var saved *dsc.DataScienceCluster
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {

		err := r.Client.Get(context.TODO(), client.ObjectKeyFromObject(original), saved)
		if err != nil {
			return err
		}
		// update status here
		saved.Status = original.Status

		// Try to update
		err = r.Client.Status().Update(context.TODO(), saved)
		// Return err itself here (not wrapped inside another error)
		// so that RetryOnConflict can identify it correctly.
		return err
	})
	if saved == nil {
		saved = original
	}
	return saved, err
}
