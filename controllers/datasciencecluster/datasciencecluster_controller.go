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

	operators "github.com/operator-framework/api/pkg/operators/v1"
	corev1 "k8s.io/api/core/v1"
	authv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/go-logr/logr"
	dsc "github.com/opendatahub-io/opendatahub-operator/apis/datasciencecluster/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/pkg/deploy"
	addonv1alpha1 "github.com/openshift/addon-operator/apis/addons/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
)

const (
	finalizer                    = "dsc-finalizer.operator.opendatahub.io"
	finalizerMaxRetries          = 10
	odhGeneratedResourceLabel    = "opendatahub.io/generated-resource"
	odhNamespace                 = "opendatahub"
	operatorGroupName            = "opendatahub-og" // hardcoded for now, but we can make it configurable later
	operatorGroupTargetNamespace = "opendatahub"    // hardcoded for now, but we can make it configurable later
)

// DataScienceClusterReconciler reconciles a DataScienceCluster object
type DataScienceClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
	// Recorder to generate events
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=datasciencecluster.opendatahub.io,resources=datascienceclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=datasciencecluster.opendatahub.io,resources=datascienceclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=datasciencecluster.opendatahub.io,resources=datascienceclusters/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=services;namespaces;serviceaccounts;secrets;configmaps;operatorgroups,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups=addons.managed.openshift.io,resources=addons,verbs=get;list
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings;roles;clusterrolebindings;clusterroles,verbs=get;list;watch;create;update;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *DataScienceClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.Log.Info("Reconciling DataScienceCluster resources", "Request.Namespace", req.Namespace, "Request.Name", req.Name)

	instance := &dsc.DataScienceCluster{}
	err := r.Client.Get(ctx, req.NamespacedName, instance)
	if err != nil && apierrs.IsNotFound(err) {
		return ctrl.Result{}, nil
	} else if err != nil {
		return ctrl.Result{}, err
	}

	// Start reconciling
	if instance.Status.Conditions == nil {
		reason := status.ReconcileInit
		message := "Initializing DataScienceCluster resource"
		status.SetProgressingCondition(&instance.Status.Conditions, reason, message)
		instance.Status.Phase = status.PhaseProgressing
		err = r.Client.Status().Update(ctx, instance)
		if err != nil {
			r.Log.Error(err, "Failed to add conditions to status of DataScienceCluster resource.", "DataScienceCluster", req.Namespace, "Request.Name", req.Name)
			// r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DataScienceClusterReconcileError",
			// 	"Failed to add conditions to DataScienceCluster instance %s", instance.Name)
			return ctrl.Result{}, err
		}
	}

	// Check if the DataScienceCluster was deleted
	deleted := instance.GetDeletionTimestamp() != nil
	finalizers := sets.NewString(instance.GetFinalizers()...)
	if deleted {
		if !finalizers.Has(finalizer) {
			r.Log.Info("DataScienceCluster instance has been deleted.", "instance", instance.Name)
			return ctrl.Result{}, nil
		}
		r.Log.Info("Deleting DataScienceCluster instance", "instance", instance.Name)

		// 	// TODO: implement delete logic

		// 	// Remove finalizer once DataScienceCluster deletion is completed.
		finalizers.Delete(finalizer)
		instance.SetFinalizers(finalizers.List())
		finalizerError := r.Client.Update(context.TODO(), instance)
		for retryCount := 0; errors.IsConflict(finalizerError) && retryCount < finalizerMaxRetries; retryCount++ {
			// Based on Istio operator at https://github.com/istio/istio/blob/master/operator/pkg/controller/istiocontrolplane/istiocontrolplane_controller.go
			// for finalizer removal errors workaround.
			r.Log.Info("Conflict during finalizer removal, retrying.")
			_ = r.Client.Get(ctx, req.NamespacedName, instance)
			finalizers = sets.NewString(instance.GetFinalizers()...)
			finalizers.Delete(finalizer)
			instance.SetFinalizers(finalizers.List())
			finalizerError = r.Client.Update(ctx, instance)
		}
		if finalizerError != nil {
			r.Log.Error(finalizerError, "error removing finalizer")
			return ctrl.Result{}, finalizerError
		}
		return ctrl.Result{}, nil
	} else if !finalizers.Has(finalizer) {
		// 	// Based on kfdef code at https://github.com/opendatahub-io/opendatahub-operator/blob/master/controllers/kfdef.apps.kubeflow.org/kfdef_controller.go#L191
		// 	// TODO: consider if this piece of logic is really needed or if we can remove it.
		r.Log.Info("Normally this should not happen. Adding the finalizer", finalizer, req)
		finalizers.Insert(finalizer)
		instance.SetFinalizers(finalizers.List())
		err = r.Client.Update(ctx, instance)
		if err != nil {
			r.Log.Error(err, "failed to update DataScienceCluster with finalizer")
			return ctrl.Result{}, err
		}
	}

	// RECONCILE LOGIC
	plan := createReconciliatioPlan(instance)

	// Extract latest Manifests
	err = deploy.DownloadManifests(instance.Spec.ManifestsUri)
	if err != nil {
		return reconcile.Result{}, err
	}

	if r.isManagedService() {
		//Apply osd specific permissions
		err = deploy.DeployManifestsFromPath(instance, r.Client,
			"/opt/odh-manifests/osd-configs",
			"redhat-ods-applications", r.Scheme)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	// reconcile components
	if err = r.createOperatorGroup(ctx, instance, plan); err != nil {
		r.Log.Error(err, "failed to reconcile DataScienceCluster (common resources)")
		// 	r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DataScienceClusterReconcileError",
		// 		"Failed to reconcile common resources on DataScienceCluster instance %s", instance.Name)
		return ctrl.Result{}, err
	}
	// if err = reconcileDashboard(instance, r.Client, r.Scheme, plan); err != nil {
	// 	r.Log.Error(err, "failed to reconcile DataScienceCluster (dashboard resources)")
	// 	r.Recorder.Eventf(instance, v1.EventTypeWarning, "DataScienceClusterReconcileError",
	// 		"Failed to reconcile dashboard resources on DataScienceCluster instance %s", instance.Name)
	// 	return ctrl.Result{}, err
	// }
	// if err = reconcileTraining(instance, r.Client, r.Scheme, plan); err != nil {
	// 	r.Log.Error(err, "failed to reconcile DataScienceCluster (training resources)")
	// 	r.Recorder.Eventf(instance, v1.EventTypeWarning, "DataScienceClusterReconcileError",
	// 		"Failed to reconcile training resources on DataScienceCluster instance %s", instance.Name)
	// 	return ctrl.Result{}, err
	// }
	// if err = reconcileServing(instance, r.Client, r.Scheme, plan); err != nil {
	// 	r.Log.Error(err, "failed to reconcile DataScienceCluster (serving resources)")
	// 	r.Recorder.Eventf(instance, v1.EventTypeWarning, "DataScienceClusterReconcileError",
	// 		"Failed to reconcile serving resources on DataScienceCluster instance %s", instance.Name)
	// 	return ctrl.Result{}, err
	// }
	// if err = reconcileWorkbench(instance, r.Client, r.Scheme, plan); err != nil {
	// 	r.Log.Error(err, "failed to reconcile DataScienceCluster (workbench resources)")
	// 	r.Recorder.Eventf(instance, v1.EventTypeWarning, "DataScienceClusterReconcileError",
	// 		"Failed to reconcile common workbench on DataScienceCluster instance %s", instance.Name)
	// 	return ctrl.Result{}, err
	// }

	// finalize reconciliation
	status.SetCompleteCondition(&instance.Status.Conditions, status.ReconcileCompleted, "DataScienceCluster resource reconciled successfully.")
	err = r.Client.Update(ctx, instance)
	if err != nil {
		r.Log.Error(err, "failed to update DataScienceCluster after successfuly completed reconciliation")
		return ctrl.Result{}, err
	} else {
		r.Log.Info("DataScienceCluster Deployment Completed.")
		// r.Recorder.Eventf(instance, corev1.EventTypeNormal, "DataScienceClusterCreationSuccessful",
		// 	"DataScienceCluster instance %s created and deployed successfully", instance.Name)
	}

	instance.Status.Phase = status.PhaseReady
	err = r.Client.Status().Update(ctx, instance)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DataScienceClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dsc.DataScienceCluster{}).
		Owns(&operators.OperatorGroup{}).
		Owns(&authv1.Role{}).
		Owns(&authv1.RoleBinding{}).
		Owns(&authv1.ClusterRole{}).
		Owns(&authv1.ClusterRoleBinding{}).
		Owns(&corev1.Namespace{}).
		Complete(r)
}

type ReconciliationPlan struct {
	Serving     bool
	Training    bool
	Workbenches bool
	Dashboard   bool
}

func createReconciliatioPlan(instance *dsc.DataScienceCluster) *ReconciliationPlan {
	plan := &ReconciliationPlan{}

	profile := instance.Spec.Profile
	if profile == "" {
		profile = dsc.ProfileFull
	}

	switch profile {
	case dsc.ProfileServing:
		// serving is enabled by default, unless explicitly overriden
		plan.Serving = instance.Spec.Components.Serving.Enabled == nil || *instance.Spec.Components.Serving.Enabled
		// training is disabled by default, unless explicitly overriden
		plan.Training = instance.Spec.Components.Training.Enabled != nil && *instance.Spec.Components.Training.Enabled
		// workbenches is disabled by default, unless explicitly overriden
		plan.Workbenches = instance.Spec.Components.Workbenches.Enabled != nil && *instance.Spec.Components.Workbenches.Enabled
		// dashboard is enabled by default, unless explicitly overriden
		plan.Dashboard = instance.Spec.Components.Dashboard.Enabled == nil || *instance.Spec.Components.Dashboard.Enabled
	case dsc.ProfileTraining:
		// serving is disabled by default, unless explicitly overriden
		plan.Serving = instance.Spec.Components.Serving.Enabled != nil && *instance.Spec.Components.Serving.Enabled
		// training is enabled by default, unless explicitly overriden
		plan.Training = instance.Spec.Components.Training.Enabled == nil || *instance.Spec.Components.Training.Enabled
		// workbenches is disabled by default, unless explicitly overriden
		plan.Workbenches = instance.Spec.Components.Workbenches.Enabled != nil && *instance.Spec.Components.Workbenches.Enabled
		// dashboard is enabled by default, unless explicitly overriden
		plan.Dashboard = instance.Spec.Components.Dashboard.Enabled == nil || *instance.Spec.Components.Dashboard.Enabled
	case dsc.ProfileWorkbench:
		// serving is disabled by default, unless explicitly overriden
		plan.Serving = instance.Spec.Components.Serving.Enabled != nil && *instance.Spec.Components.Serving.Enabled
		// training is disabled by default, unless explicitly overriden
		plan.Training = instance.Spec.Components.Training.Enabled != nil && *instance.Spec.Components.Training.Enabled
		// workbenches is enabled by default, unless explicitly overriden
		plan.Workbenches = instance.Spec.Components.Workbenches.Enabled == nil || *instance.Spec.Components.Workbenches.Enabled
		// dashboard is enabled by default, unless explicitly overriden
		plan.Dashboard = instance.Spec.Components.Dashboard.Enabled == nil || *instance.Spec.Components.Dashboard.Enabled
	case dsc.ProfileFull:
		// serving is enabled by default, unless explicitly overriden
		plan.Serving = instance.Spec.Components.Serving.Enabled == nil || *instance.Spec.Components.Serving.Enabled
		// training is enabled by default, unless explicitly overriden
		plan.Training = instance.Spec.Components.Training.Enabled == nil || *instance.Spec.Components.Training.Enabled
		// workbenches is enabled by default, unless explicitly overriden
		plan.Workbenches = instance.Spec.Components.Workbenches.Enabled == nil || *instance.Spec.Components.Workbenches.Enabled
		// dashboard is enabled by default, unless explicitly overriden
		plan.Dashboard = instance.Spec.Components.Dashboard.Enabled == nil || *instance.Spec.Components.Dashboard.Enabled
	}

	return plan
}

// TODO: should we generalize this function and move it to a common place?
func (r *DataScienceClusterReconciler) isManagedService() bool {
	expectedAddon := &addonv1alpha1.Addon{}
	err := r.Client.Get(context.TODO(), client.ObjectKey{Name: "managed-odh"}, expectedAddon)
	if err != nil {
		if apierrs.IsNotFound(err) {
			return false
		} else {
			r.Log.Error(err, "error getting Addon instance for managed service")
			return false
		}
	}
	return true
}

func (r *DataScienceClusterReconciler) createOperatorGroup(ctx context.Context, instance *dsc.DataScienceCluster, plan *ReconciliationPlan) error {

	// check if operator group already exists
	r.Log.Info("Reconciling the OperatorGroup", "name", operatorGroupName)
	foundOperatorGroup := &operators.OperatorGroup{}
	err := r.Get(ctx, client.ObjectKey{Name: operatorGroupName}, foundOperatorGroup)
	if err != nil {
		if apierrs.IsNotFound(err) {
			r.Log.Info("Creating OperatorGroup", "name", operatorGroupName)

			operatorGroup := &operators.OperatorGroup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      operatorGroupName,
					Namespace: odhNamespace,
					Labels: map[string]string{
						odhGeneratedResourceLabel: "true",
					},
				},
				Spec: operators.OperatorGroupSpec{
					TargetNamespaces: []string{operatorGroupTargetNamespace},
				},
			}
			// Set Controller reference
			err = ctrl.SetControllerReference(instance, operatorGroup, r.Scheme)
			if err != nil {
				r.Log.Error(err, "Unable to add OwnerReference to the OperatorGroup", "name", operatorGroupName)
				return err
			}
			err = r.Create(ctx, operatorGroup)
			if err != nil && !apierrs.IsAlreadyExists(err) {
				r.Log.Error(err, "Unable to create OperatorGroup", "name", operatorGroupName)
				return err
			}
		} else {
			r.Log.Error(err, "Unable to fetch OperatorGroup", "name", operatorGroupName)
			return err
		}
	}
	return nil
}

// func reconcileWorkbench(instance *dsc.DataScienceCluster, client client.Client, scheme *runtime.Scheme, plan *ReconciliationPlan) error {
// 	// check if we need to apply the resources or if they already exist
// 	if plan.Dashboard {
// 		err := deploy.DeployManifestsFromPath(instance, client,
// 			"/opt/odh-manifests/odh-dashboard/base",
// 			"opendatahub",
// 			scheme)
// 		return err
// 	}
// 	return nil
// }

// func reconcileServing(instance *dsc.DataScienceCluster, client client.Client, scheme *runtime.Scheme, plan *ReconciliationPlan) error {
// 	panic("unimplemented")
// }

// func reconcileTraining(instance *dsc.DataScienceCluster, client client.Client, scheme *runtime.Scheme, plan *ReconciliationPlan) error {
// 	panic("unimplemented")
// }

// func reconcileDashboard(instance *dsc.DataScienceCluster, client client.Client, scheme *runtime.Scheme, plan *ReconciliationPlan) error {
// 	panic("unimplemented")
// }
