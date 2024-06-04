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

// Package datasciencecluster contains controller logic of CRD DataScienceCluster
package datasciencecluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/hashicorp/go-multierror"
	ocbuildv1 "github.com/openshift/api/build/v1"
	ocimgv1 "github.com/openshift/api/image/v1"
	v1 "github.com/openshift/api/operator/v1"
	admv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	authv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	dsc "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/datasciencepipelines"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
)

// DataScienceClusterReconciler reconciles a DataScienceCluster object.
type DataScienceClusterReconciler struct { //nolint:golint,revive
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
	// Recorder to generate events
	Recorder           record.EventRecorder
	DataScienceCluster *DataScienceClusterConfig
}

// DataScienceClusterConfig passing Spec of DSCI for reconcile DataScienceCluster.
type DataScienceClusterConfig struct { //nolint:golint,revive
	DSCISpec *dsci.DSCInitializationSpec
}

const (
	finalizerName = "datasciencecluster.opendatahub.io/finalizer"
)

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *DataScienceClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) { //nolint:maintidx,gocyclo
	r.Log.Info("Reconciling DataScienceCluster resources", "Request.Name", req.Name)

	// Get information on version
	currentOperatorReleaseVersion, err := cluster.GetRelease(r.Client)
	if err != nil {
		r.Log.Error(err, "failed to get operator release version")
		return ctrl.Result{}, err
	}

	instances := &dsc.DataScienceClusterList{}

	if err := r.Client.List(ctx, instances); err != nil {
		return ctrl.Result{}, err
	}

	if len(instances.Items) == 0 {
		// Request object not found, could have been deleted after reconcile request.
		// Owned objects are automatically garbage collected.
		// For additional cleanup logic use operatorUninstall function.
		// Return and don't requeue
		if upgrade.HasDeleteConfigMap(ctx, r.Client) {
			if uninstallErr := upgrade.OperatorUninstall(ctx, r.Client); uninstallErr != nil {
				return ctrl.Result{}, fmt.Errorf("error while operator uninstall: %w", uninstallErr)
			}
		}

		return ctrl.Result{}, nil
	}

	instance := &instances.Items[0]

	allComponents, err := instance.GetComponents()
	if err != nil {
		return ctrl.Result{}, err
	}

	// If DSC CR exist and deletion CM exist
	// delete DSC CR and let reconcile requeue
	// sometimes with finalzier DSC CR wont get deleted, force to remove finalizer here
	if upgrade.HasDeleteConfigMap(ctx, r.Client) {
		if controllerutil.ContainsFinalizer(instance, finalizerName) {
			if controllerutil.RemoveFinalizer(instance, finalizerName) {
				if err := r.Update(ctx, instance); err != nil {
					r.Log.Info("Error to remove DSC finalzier", "error", err)
					return ctrl.Result{}, err
				}
				r.Log.Info("Removed finalizer for DataScienceCluster", "name", instance.Name, "finalizer", finalizerName)
			}
		}
		if err := r.Client.Delete(ctx, instance, []client.DeleteOption{}...); err != nil {
			if !apierrs.IsNotFound(err) {
				return reconcile.Result{}, err
			}
		}
		for _, component := range allComponents {
			if err := component.Cleanup(r.Client, r.DataScienceCluster.DSCISpec); err != nil {
				return ctrl.Result{}, err
			}
		}
		return reconcile.Result{Requeue: true}, nil
	}

	// Verify a valid DSCInitialization instance is created
	dsciInstances := &dsci.DSCInitializationList{}
	err = r.Client.List(ctx, dsciInstances)
	if err != nil {
		r.Log.Error(err, "Failed to retrieve DSCInitialization resource.", "DSCInitialization Request.Name", req.Name)
		r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DSCInitializationReconcileError", "Failed to retrieve DSCInitialization instance")

		return ctrl.Result{}, err
	}

	// Update phase to error state if DataScienceCluster is created without valid DSCInitialization
	switch len(dsciInstances.Items) { // only handle number as 0 or 1, others won't be existed since webhook block creation
	case 0:
		reason := status.ReconcileFailed
		message := "Failed to get a valid DSCInitialization instance, please create a DSCI instance"
		r.Log.Info(message)
		instance, err = status.UpdateWithRetry(ctx, r.Client, instance, func(saved *dsc.DataScienceCluster) {
			status.SetProgressingCondition(&saved.Status.Conditions, reason, message)
			// Patch Degraded with True status
			status.SetGeneralCondition(&saved.Status.Conditions, "Degraded", reason, message, corev1.ConditionTrue)
			saved.Status.Phase = status.PhaseError
		})
		if err != nil {
			r.reportError(err, instance, "failed to update DataScienceCluster condition")

			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	case 1:
		dscInitializationSpec := dsciInstances.Items[0].Spec
		dscInitializationSpec.DeepCopyInto(r.DataScienceCluster.DSCISpec)
	}

	if instance.ObjectMeta.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(instance, finalizerName) {
			r.Log.Info("Adding finalizer for DataScienceCluster", "name", instance.Name, "finalizer", finalizerName)
			controllerutil.AddFinalizer(instance, finalizerName)
			if err := r.Update(ctx, instance); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		r.Log.Info("Finalization DataScienceCluster start deleting instance", "name", instance.Name, "finalizer", finalizerName)
		for _, component := range allComponents {
			if err := component.Cleanup(r.Client, r.DataScienceCluster.DSCISpec); err != nil {
				return ctrl.Result{}, err
			}
		}
		if controllerutil.ContainsFinalizer(instance, finalizerName) {
			controllerutil.RemoveFinalizer(instance, finalizerName)
			if err := r.Update(ctx, instance); err != nil {
				return ctrl.Result{}, err
			}
		}
		if upgrade.HasDeleteConfigMap(ctx, r.Client) {
			// if delete configmap exists, requeue the request to handle operator uninstall
			return reconcile.Result{Requeue: true}, nil
		}

		return ctrl.Result{}, nil
	}
	// Check preconditions if this is an upgrade
	if instance.Status.Phase == status.PhaseReady {
		// Check for existence of Argo Workflows if DSP is
		if instance.Status.InstalledComponents[datasciencepipelines.ComponentName] {
			if err := datasciencepipelines.UnmanagedArgoWorkFlowExists(ctx, r.Client); err != nil {
				message := fmt.Sprintf("Failed upgrade: %v ", err.Error())
				_, err = status.UpdateWithRetry(ctx, r.Client, instance, func(saved *dsc.DataScienceCluster) {
					status.SetExistingArgoCondition(&saved.Status.Conditions, status.ArgoWorkflowExist, message)
					status.SetErrorCondition(&saved.Status.Conditions, status.ArgoWorkflowExist, message)
					saved.Status.Phase = status.PhaseError
				})
				return ctrl.Result{}, err
			}
		}
	}

	// Start reconciling
	if instance.Status.Conditions == nil {
		reason := status.ReconcileInit
		message := "Initializing DataScienceCluster resource"
		instance, err = status.UpdateWithRetry(ctx, r.Client, instance, func(saved *dsc.DataScienceCluster) {
			status.SetProgressingCondition(&saved.Status.Conditions, reason, message)
			saved.Status.Phase = status.PhaseProgressing
		})
		if err != nil {
			_ = r.reportError(err, instance, fmt.Sprintf("failed to add conditions to status of DataScienceCluster resource name %s", req.Name))

			return ctrl.Result{}, err
		}
	}

	// Initialize error list, instead of returning errors after every component is deployed
	var componentErrors *multierror.Error

	for _, component := range allComponents {
		if instance, err = r.reconcileSubComponent(ctx, instance, component); err != nil {
			componentErrors = multierror.Append(componentErrors, err)
		}
	}

	// Process errors for components
	if componentErrors != nil {
		r.Log.Info("DataScienceCluster Deployment Incomplete.")
		instance, err = status.UpdateWithRetry(ctx, r.Client, instance, func(saved *dsc.DataScienceCluster) {
			status.SetCompleteCondition(&saved.Status.Conditions, status.ReconcileCompletedWithComponentErrors,
				fmt.Sprintf("DataScienceCluster resource reconciled with component errors: %v", componentErrors))
			saved.Status.Phase = status.PhaseReady
		})
		if err != nil {
			r.Log.Error(err, "failed to update DataScienceCluster conditions with incompleted reconciliation")

			return ctrl.Result{}, err
		}
		r.Recorder.Eventf(instance, corev1.EventTypeNormal, "DataScienceClusterComponentFailures",
			"DataScienceCluster instance %s created, but have some failures in component %v", instance.Name, componentErrors)

		return ctrl.Result{RequeueAfter: time.Second * 30}, componentErrors
	}

	// finalize reconciliation
	instance, err = status.UpdateWithRetry(ctx, r.Client, instance, func(saved *dsc.DataScienceCluster) {
		status.SetCompleteCondition(&saved.Status.Conditions, status.ReconcileCompleted, "DataScienceCluster resource reconciled successfully")
		saved.Status.Phase = status.PhaseReady
		saved.Status.Release = currentOperatorReleaseVersion
	})

	if err != nil {
		r.Log.Error(err, "failed to update DataScienceCluster conditions after successfully completed reconciliation")

		return ctrl.Result{}, err
	}

	r.Log.Info("DataScienceCluster Deployment Completed.")
	r.Recorder.Eventf(instance, corev1.EventTypeNormal, "DataScienceClusterCreationSuccessful",
		"DataScienceCluster instance %s created and deployed successfully", instance.Name)

	return ctrl.Result{}, nil
}

func (r *DataScienceClusterReconciler) reconcileSubComponent(ctx context.Context, instance *dsc.DataScienceCluster,
	component components.ComponentInterface,
) (*dsc.DataScienceCluster, error) {
	componentName := component.GetComponentName()

	enabled := component.GetManagementState() == v1.Managed
	installedComponentValue, isExistStatus := instance.Status.InstalledComponents[componentName]

	// First set conditions to reflect a component is about to be reconciled
	// only set to init condition e.g Unknonw for the very first time when component is not in the list
	if !isExistStatus {
		message := "Component is disabled"
		if enabled {
			message = "Component is enabled"
		}
		instance, err := status.UpdateWithRetry(ctx, r.Client, instance, func(saved *dsc.DataScienceCluster) {
			status.SetComponentCondition(&saved.Status.Conditions, componentName, status.ReconcileInit, message, corev1.ConditionUnknown)
		})
		if err != nil {
			_ = r.reportError(err, instance, "failed to update DataScienceCluster conditions before first time reconciling "+componentName)
			// try to continue with reconciliation, as further updates can fix the status
		}
	}
	// Reconcile component
	err := component.ReconcileComponent(ctx, r.Client, r.Log, instance, r.DataScienceCluster.DSCISpec, installedComponentValue)

	if err != nil {
		// reconciliation failed: log errors, raise event and update status accordingly
		instance = r.reportError(err, instance, "failed to reconcile "+componentName+" on DataScienceCluster")
		instance, _ = status.UpdateWithRetry(ctx, r.Client, instance, func(saved *dsc.DataScienceCluster) {
			if enabled {
				if strings.Contains(err.Error(), datasciencepipelines.ArgoWorkflowCRD+" CRD already exists") {
					status.SetExistingArgoCondition(&saved.Status.Conditions, status.ArgoWorkflowExist, fmt.Sprintf("Component update failed: %v", err))
				} else {
					status.SetComponentCondition(&saved.Status.Conditions, componentName, status.ReconcileFailed, fmt.Sprintf("Component reconciliation failed: %v", err), corev1.ConditionFalse)
				}
			} else {
				status.SetComponentCondition(&saved.Status.Conditions, componentName, status.ReconcileFailed, fmt.Sprintf("Component removal failed: %v", err), corev1.ConditionFalse)
			}
		})
		return instance, err
	}
	// reconciliation succeeded: update status accordingly
	instance, err = status.UpdateWithRetry(ctx, r.Client, instance, func(saved *dsc.DataScienceCluster) {
		if saved.Status.InstalledComponents == nil {
			saved.Status.InstalledComponents = make(map[string]bool)
		}
		saved.Status.InstalledComponents[componentName] = enabled
		if enabled {
			status.SetComponentCondition(&saved.Status.Conditions, componentName, status.ReconcileCompleted, "Component reconciled successfully", corev1.ConditionTrue)
		} else {
			status.RemoveComponentCondition(&saved.Status.Conditions, componentName)
		}
	})
	if err != nil {
		instance = r.reportError(err, instance, "failed to update DataScienceCluster status after reconciling "+componentName)

		return instance, err
	}

	return instance, nil
}

func (r *DataScienceClusterReconciler) reportError(err error, instance *dsc.DataScienceCluster, message string) *dsc.DataScienceCluster {
	r.Log.Error(err, message, "instance.Name", instance.Name)
	r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DataScienceClusterReconcileError",
		"%s for instance %s", message, instance.Name)
	return instance
}

var configMapPredicates = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		// Do not reconcile on prometheus configmap update, since it is handled by DSCI
		if e.ObjectNew.GetName() == "prometheus" && e.ObjectNew.GetNamespace() == "redhat-ods-monitoring" {
			return false
		}
		// Do not reconcile on kserver's inferenceservice-config CM updates, for rawdeployment
		if e.ObjectNew.GetName() == "inferenceservice-config" && (e.ObjectNew.GetNamespace() == "redhat-ods-applications" || e.ObjectNew.GetNamespace() == "opendatahub") {
			return false
		}
		return true
	},
}

// a workaround for 2.5 due to odh-model-controller serivceaccount keeps updates with label.
var saPredicates = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		if e.ObjectNew.GetName() == "odh-model-controller" && (e.ObjectNew.GetNamespace() == "redhat-ods-applications" || e.ObjectNew.GetNamespace() == "opendatahub") {
			return false
		}
		return true
	},
}

// a workaround for 2.5 due to modelmesh-servingruntime.serving.kserve.io keeps updates.
var modelMeshwebhookPredicates = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		return e.ObjectNew.GetName() != "modelmesh-servingruntime.serving.kserve.io"
	},
}

var modelMeshRolePredicates = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		notAllowedNames := []string{"leader-election-role", "proxy-role", "metrics-reader", "kserve-prometheus-k8s", "odh-model-controller-role"}
		for _, notallowedName := range notAllowedNames {
			if e.ObjectNew.GetName() == notallowedName {
				return false
			}
		}
		return true
	},
}

var modelMeshRBPredicates = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		notAllowedNames := []string{"leader-election-rolebinding", "proxy-rolebinding", "odh-model-controller-rolebinding-opendatahub"}
		for _, notallowedName := range notAllowedNames {
			if e.ObjectNew.GetName() == notallowedName {
				return false
			}
		}
		return true
	},
}

// ignore label updates if it is from application namespace.
var modelMeshGeneralPredicates = predicate.Funcs{
	UpdateFunc: func(e event.UpdateEvent) bool {
		if strings.Contains(e.ObjectNew.GetName(), "odh-model-controller") || strings.Contains(e.ObjectNew.GetName(), "kserve") {
			return false
		}
		return true
	},
}

// SetupWithManager sets up the controller with the Manager.
func (r *DataScienceClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dsc.DataScienceCluster{}).
		Owns(&corev1.Namespace{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.ConfigMap{}, builder.WithPredicates(configMapPredicates)).
		Owns(&netv1.NetworkPolicy{}).
		Owns(&authv1.Role{}, builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, modelMeshRolePredicates))).
		Owns(&authv1.RoleBinding{}, builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, modelMeshRBPredicates))).
		Owns(&authv1.ClusterRole{}, builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, modelMeshRolePredicates))).
		Owns(&authv1.ClusterRoleBinding{}, builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, modelMeshRBPredicates))).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&corev1.Service{}, builder.WithPredicates(predicate.Or(predicate.GenerationChangedPredicate{}, modelMeshGeneralPredicates))).
		Owns(&appsv1.StatefulSet{}).
		Owns(&ocimgv1.ImageStream{}).
		Owns(&ocbuildv1.BuildConfig{}).
		Owns(&apiregistrationv1.APIService{}).
		Owns(&netv1.Ingress{}).
		Owns(&admv1.MutatingWebhookConfiguration{}).
		Owns(&admv1.ValidatingWebhookConfiguration{}, builder.WithPredicates(modelMeshwebhookPredicates)).
		Owns(&corev1.ServiceAccount{}, builder.WithPredicates(saPredicates)).
		Watches(&source.Kind{Type: &dsci.DSCInitialization{}}, handler.EnqueueRequestsFromMapFunc(r.watchDataScienceClusterForDSCI)).
		Watches(&source.Kind{Type: &corev1.ConfigMap{}}, handler.EnqueueRequestsFromMapFunc(r.watchDataScienceClusterResources), builder.WithPredicates(configMapPredicates)).
		Watches(&source.Kind{Type: &apiextensionsv1.CustomResourceDefinition{}}, handler.EnqueueRequestsFromMapFunc(r.watchDataScienceClusterResources),
			builder.WithPredicates(argoWorkflowCRDPredicates)).
		// this predicates prevents meaningless reconciliations from being triggered
		WithEventFilter(predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{})).
		Complete(r)
}

func (r *DataScienceClusterReconciler) watchDataScienceClusterForDSCI(a client.Object) []reconcile.Request {
	instanceList := &dsc.DataScienceClusterList{}
	err := r.Client.List(context.TODO(), instanceList)
	if err != nil {
		return nil
	}
	var requestName string
	switch {
	case len(instanceList.Items) == 1:
		requestName = instanceList.Items[0].Name
	case len(instanceList.Items) == 0:
		requestName = "default-dsc"
	default:
		return nil
	}
	// When DSCI CR gets created, trigger reconcile function
	if a.GetObjectKind().GroupVersionKind().Kind == "DSCInitialization" || a.GetName() == "default-dsci" {
		return []reconcile.Request{{
			NamespacedName: types.NamespacedName{Name: requestName},
		}}
	}
	return nil
}
func (r *DataScienceClusterReconciler) watchDataScienceClusterResources(a client.Object) []reconcile.Request {
	instanceList := &dsc.DataScienceClusterList{}
	err := r.Client.List(context.TODO(), instanceList)
	if err != nil {
		return nil
	}
	var requestName string
	switch {
	case len(instanceList.Items) == 1:
		requestName = instanceList.Items[0].Name
	case len(instanceList.Items) == 0:
		requestName = "default-dsc"
	default:
		return nil
	}

	// Trigger reconcile function when uninstall configmap is created
	operatorNs, err := cluster.GetOperatorNamespace()
	if err != nil {
		return nil
	}
	if a.GetNamespace() == operatorNs {
		labels := a.GetLabels()
		if val, ok := labels[upgrade.DeleteConfigMapLabel]; ok && val == "true" {
			return []reconcile.Request{{
				NamespacedName: types.NamespacedName{Name: requestName},
			}}
		}
		return nil
	}

	// Trigger reconcile function when DSCInitialization from Missing to Valid
	if a.GetObjectKind().GroupVersionKind().Kind == "DSCInitialization" {
		return []reconcile.Request{{
			NamespacedName: types.NamespacedName{Name: requestName},
		}}
	}

	return nil
}

// argoWorkflowCRDPredicates filters the delete events to trigger reconcile when Argo Workflow CRD is deleted.
var argoWorkflowCRDPredicates = predicate.Funcs{
	DeleteFunc: func(e event.DeleteEvent) bool {
		if e.Object.GetName() == datasciencepipelines.ArgoWorkflowCRD {
			labelList := e.Object.GetLabels()
			// CRD to be deleted with label "app.opendatahub.io/datasciencepipeline":"true", should not trigger reconcile
			if value, exist := labelList[labels.ODH.Component(datasciencepipelines.ComponentName)]; exist && value == "true" {
				return false
			}
		}
		// CRD to be deleted either not with label or label value is not "true", should trigger reconcile
		return true
	},
}
