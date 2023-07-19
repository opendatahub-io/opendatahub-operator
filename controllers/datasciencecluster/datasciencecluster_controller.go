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
	dsc "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/datasciencepipelines"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/distributedworkloads"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/kserve"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/modelmeshserving"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/workbenches"
	appsv1 "k8s.io/api/apps/v1"
	netv1 "k8s.io/api/networking/v1"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"time"

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

	if len(instanceList.Items) > 1 {
		message := fmt.Sprintf("only one instance of DataScienceCluster object is allowed. Update existing instance on namespace %s and name %s", req.Namespace, req.Name)
		r.reportError(err, &instanceList.Items[0], ctx, message)
		return ctrl.Result{}, fmt.Errorf(message)
	}

	// Start reconciling
	if instance.Status.Conditions == nil {
		reason := status.ReconcileInit
		message := "Initializing DataScienceCluster resource"
		status.SetProgressingCondition(&instance.Status.Conditions, reason, message)
		instance.Status.Phase = status.PhaseProgressing
		err = r.Client.Status().Update(ctx, instance)
		if err != nil {
			r.reportError(err, instance, ctx, fmt.Sprintf("failed to add conditions to status of DataScienceCluster resource on namespace %s and name %s", req.Namespace, req.Name))
			return ctrl.Result{}, err
		}
	}
	if instance.Status.InstalledComponents == nil {
		instance.Status.InstalledComponents = make(map[string]bool)
	}

	// reconcile dashboard component
	enabled := r.isComponentEnabled(&(instance.Spec.Components.Dashboard))
	if val, err := r.reconcileSubComponent(instance, dashboard.ComponentName, enabled, &(instance.Spec.Components.Dashboard), ctx); err != nil {
		// Update immediately only when error occurs, else skip to end
		err := r.Client.Update(ctx, instance)
		if err != nil {
			r.Log.Info(fmt.Sprintf("failed to set DataScienceCluster component %s to enabled: false", dashboard.ComponentName), "error", err)
			// no need to return error as this is not critical and will be reconciled in the next update or reconcile loop
		}
		// no need to log any errors as this is done in the reconcileSubComponent method
		return val, err
	}

	// reconcile DataSciencePipelines component
	enabled = r.isComponentEnabled(&(instance.Spec.Components.DataSciencePipelines))
	if val, err := r.reconcileSubComponent(instance, datasciencepipelines.ComponentName, enabled, &(instance.Spec.Components.DataSciencePipelines), ctx); err != nil {
		// Update immediately only when error occurs, else skip to end
		err := r.Client.Update(ctx, instance)
		if err != nil {
			r.Log.Info(fmt.Sprintf("failed to set DataScienceCluster component %s to enabled: false", datasciencepipelines.ComponentName), "error", err)
			// no need to return error as this is not critical and will be reconciled in the next update or reconcile loop
		}
		// no need to log any errors as this is done in the reconcileSubComponent method
		return val, err
	}

	// reconcile ModelMesh component
	enabled = r.isComponentEnabled(&(instance.Spec.Components.ModelMeshServing))
	if val, err := r.reconcileSubComponent(instance, modelmeshserving.ComponentName, enabled, &(instance.Spec.Components.ModelMeshServing), ctx); err != nil {
		// Update immediately only when error occurs, else skip to end
		err := r.Client.Update(ctx, instance)
		if err != nil {
			r.Log.Info(fmt.Sprintf("failed to set DataScienceCluster component %s to enabled: false", modelmeshserving.ComponentName), "error", err)
			// no need to return error as this is not critical and will be reconciled in the next update or reconcile loop
		}
		// no need to log any errors as this is done in the reconcileSubComponent method
		return val, err
	}

	// reconcile Workbench component
	enabled = r.isComponentEnabled(&(instance.Spec.Components.Workbenches))
	if val, err := r.reconcileSubComponent(instance, workbenches.ComponentName, enabled, &(instance.Spec.Components.Workbenches), ctx); err != nil {
		// Update immediately only when error occurs, else skip to end
		err := r.Client.Update(ctx, instance)
		if err != nil {
			r.Log.Info(fmt.Sprintf("failed to set DataScienceCluster component %s to enabled: false", workbenches.ComponentName), "error", err)
			// no need to return error as this is not critical and will be reconciled in the next update or reconcile loop
		}
		// no need to log any errors as this is done in the reconcileSubComponent method
		return val, err
	}

	// reconcile Kserve component
	enabled = r.isComponentEnabled(&(instance.Spec.Components.Kserve))
	if val, err := r.reconcileSubComponent(instance, kserve.ComponentName, enabled, &(instance.Spec.Components.Kserve), ctx); err != nil {
		err := r.Client.Update(ctx, instance)
		if err != nil {
			r.Log.Info(fmt.Sprintf("failed to set DataScienceCluster component %s to enabled: false", kserve.ComponentName), "error", err)
			// no need to return error as this is not critical and will be reconciled in the next update or reconcile loop
		}
		// no need to log any errors as this is done in the reconcileSubComponent method
		return val, err
	}

	// reconcile DistributedWorkloads component
	enabled = r.isComponentEnabled(&(instance.Spec.Components.DistributeWorkloads))
	if val, err := r.reconcileSubComponent(instance, distributedworkloads.ComponentName, enabled, &(instance.Spec.Components.DistributeWorkloads), ctx); err != nil {
		err := r.Client.Update(ctx, instance)
		if err != nil {
			r.Log.Info(fmt.Sprintf("failed to set DataScienceCluster component %s to enabled: false", distributedworkloads.ComponentName), "error", err)
			// no need to return error as this is not critical and will be reconciled in the next update or reconcile loop
		}
		// no need to log any errors as this is done in the reconcileSubComponent method
		return val, err
	}

	// Update final state of spec
	err = r.Client.Update(ctx, instance)
	if err != nil {
		r.Log.Info(fmt.Sprintf("failed to set DataScienceCluster CR :%v", instance.Name), "error", err)
		// no need to return error as this is not critical and will be reconciled in the next update or reconcile loop
	}
	// finalize reconciliation
	status.SetCompleteCondition(&instance.Status.Conditions, status.ReconcileCompleted, "DataScienceCluster resource reconciled successfully.")
	err = r.Client.Status().Update(ctx, instance)
	if err != nil {
		r.Log.Error(err, "failed to update DataScienceCluster conditions after successfuly completed reconciliation")
		return ctrl.Result{}, err
	} else {
		r.Log.Info("DataScienceCluster Deployment Completed.")
		r.Recorder.Eventf(instance, corev1.EventTypeNormal, "DataScienceClusterCreationSuccessful",
			"DataScienceCluster instance %s created and deployed successfully", instance.Name)
	}

	instance.Status.Phase = status.PhaseReady
	if err = r.Client.Status().Update(ctx, instance); err != nil {
		r.Log.Error(err, "failed to update DataScienceCluster status after successfuly completed reconciliation")
	}

	return ctrl.Result{}, nil
}

func (r *DataScienceClusterReconciler) isComponentEnabled(component components.ComponentInterface) bool {
	if component.IsEnabled() == nil {
		component.SetEnabled(false)
	}
	return *component.IsEnabled()
}

func (r *DataScienceClusterReconciler) reconcileSubComponent(instance *dsc.DataScienceCluster, componentName string, enabled bool,
	component components.ComponentInterface, ctx context.Context) (ctrl.Result, error) {
	if err := component.ReconcileComponent(instance, r.Client, r.Scheme, enabled, r.ApplicationsNamespace); err != nil {
		r.reportError(err, instance, ctx, "failed to reconcile "+componentName+" on DataScienceCluster")
		return ctrl.Result{
			// Retry after failure until success.
			RequeueAfter: time.Second * 10,
		}, err
	}
	instance.Status.InstalledComponents[componentName] = enabled
	if err := r.Client.Status().Update(ctx, instance); err != nil {
		r.reportError(err, instance, ctx, "failed to update DataScienceCluster status after reconciling "+componentName)
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *DataScienceClusterReconciler) reportError(err error, instance *dsc.DataScienceCluster, ctx context.Context, message string) {
	r.Log.Error(err, message, "instance.Name", instance.Name)
	r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DataScienceClusterReconcileError",
		"%s for instance %s", message, instance.Name)
	status.SetErrorCondition(&instance.Status.Conditions, status.ReconcileFailed, fmt.Sprintf("%s : %v", message, err))
	instance.Status.Phase = status.PhaseError
	if err = r.Client.Status().Update(ctx, instance); err != nil {
		r.Log.Error(err, "failed to update DataScienceCluster status after error", "instance.Name", instance.Name)
	}
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
