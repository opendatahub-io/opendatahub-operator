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
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	dsc "github.com/opendatahub-io/opendatahub-operator/apis/datasciencecluster/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/components/datasciencepipelines"
	"github.com/opendatahub-io/opendatahub-operator/components/modelmeshserving"
	"github.com/opendatahub-io/opendatahub-operator/components/profiles"
	"github.com/opendatahub-io/opendatahub-operator/components/workbenches"
	"github.com/opendatahub-io/opendatahub-operator/controllers/status"
	corev1 "k8s.io/api/core/v1"
	authv1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	finalizer                 = "dsc-finalizer.operator.opendatahub.io"
	finalizerMaxRetries       = 10
	odhGeneratedResourceLabel = "opendatahub.io/generated-resource" // hardcoded for now, but we can make it configurable later
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

	// Return if multiple instances of DataScienceCluster exist
	instanceList := &dsc.DataScienceClusterList{}
	err := r.Client.List(context.TODO(), instanceList)
	if err != nil && apierrs.IsNotFound(err) {
		return ctrl.Result{}, nil
	} else if err != nil {
		return ctrl.Result{}, err
	}
	if len(instanceList.Items) > 1 {
		return ctrl.Result{}, fmt.Errorf("only one instance of DataScienceCluster object is allowed. Update existing instance")
	}

	instance := &instanceList.Items[0]

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

	// Create reconciliation plan i.e identify which components need to be enabled
	plan := CreateReconciliationPlan(instance)
	if err = instance.Spec.Components.Dashboard.ReconcileComponent(instance, r.Client, r.Scheme, plan.Components[dashboard.ComponentName], r.ApplicationsNamespace); err != nil {
		r.Log.Error(err, "failed to reconcile DataScienceCluster (dashboard resources)")
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DataScienceClusterReconcileError",
			"Failed to reconcile dashboard resources on DataScienceCluster instance %s", instance.Name)
		status.SetErrorCondition(&instance.Status.Conditions, status.ReconcileFailed, fmt.Sprintf("Dashboard failed to deploy : %v", err))
		instance.Status.Phase = status.PhaseError
		err = r.Client.Status().Update(ctx, instance)
		return ctrl.Result{}, err
	}
	if err = instance.Spec.Components.DataSciencePipelines.ReconcileComponent(instance, r.Client, r.Scheme, plan.Components[datasciencepipelines.ComponentName], r.ApplicationsNamespace); err != nil {
		r.Log.Error(err, "failed to reconcile DataScienceCluster (DataSciencePipelines)")
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DataScienceClusterReconcileError",
			"Failed to reconcile training resources on DataScienceCluster instance %s", instance.Name)
		status.SetErrorCondition(&instance.Status.Conditions, status.ReconcileFailed, fmt.Sprintf("DataSciencePipelines failed to deploy : %v", err))
		instance.Status.Phase = status.PhaseError
		err = r.Client.Status().Update(ctx, instance)
		return ctrl.Result{}, err
	}
	if err = instance.Spec.Components.ModelMeshServing.ReconcileComponent(instance, r.Client, r.Scheme, plan.Components[modelmeshserving.ComponentName], r.ApplicationsNamespace); err != nil {
		r.Log.Error(err, "failed to reconcile DataScienceCluster (modelmesh serving resources)")
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DataScienceClusterReconcileError",
			"Failed to reconcile serving resources on DataScienceCluster instance %s", instance.Name)
		status.SetErrorCondition(&instance.Status.Conditions, status.ReconcileFailed, fmt.Sprintf("Modelmesh serving failed to deploy : %v", err))
		instance.Status.Phase = status.PhaseError
		err = r.Client.Status().Update(ctx, instance)
		return ctrl.Result{}, err
	}
	if err = instance.Spec.Components.Workbenches.ReconcileComponent(instance, r.Client, r.Scheme, plan.Components[workbenches.ComponentName], r.ApplicationsNamespace); err != nil {
		r.Log.Error(err, "failed to reconcile DataScienceCluster (workbench resources)")
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DataScienceClusterReconcileError",
			"Failed to reconcile common workbench on DataScienceCluster instance %s", instance.Name)
		status.SetErrorCondition(&instance.Status.Conditions, status.ReconcileFailed, fmt.Sprintf("Modelmesh serving failed to deploy : %v", err))
		instance.Status.Phase = status.PhaseError
		err = r.Client.Status().Update(ctx, instance)
		return ctrl.Result{}, err
	}

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

func CreateReconciliationPlan(instance *dsc.DataScienceCluster) *profiles.ReconciliationPlan {
	plan := &profiles.ReconciliationPlan{Components: make(map[string]bool)}

	profile := instance.Spec.Profile
	if profile == "" {
		profile = profiles.ProfileCore
	}

	// Set profile defaults
	profiles.ProfileConfigs = profiles.SetDefaultProfiles()
	// Create plan for component deployment
	profiles.PopulatePlan(profiles.ProfileConfigs[profile], plan, instance)
	// Similarly set other profiles
	return plan
}
