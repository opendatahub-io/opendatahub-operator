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

	operatorv1 "github.com/openshift/api/operator/v1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	"golang.org/x/exp/slices"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/opendatahub-io/opendatahub-operator/v2/apis/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/pkg/componentsregistry"
	odhClient "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/dependent"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
)

// DataScienceClusterReconciler reconciles a DataScienceCluster object.
type DataScienceClusterReconciler struct {
	*odhClient.Client
	Scheme *runtime.Scheme
	// Recorder to generate events
	Recorder record.EventRecorder
}

const (
	finalizerName = "datasciencecluster.opendatahub.io/finalizer"
	fieldOwner    = "datasciencecluster.opendatahub.io"
)

// TODO: all the logic about the deletion configmap should be moved to another controller
//       https://issues.redhat.com/browse/RHOAIENG-16674

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *DataScienceClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithName("DataScienceCluster")
	log.Info("Reconciling DataScienceCluster resources", "Request.Name", req.Name)

	instance := &dscv1.DataScienceCluster{}
	err := r.Client.Get(ctx, req.NamespacedName, instance)

	switch {
	case k8serr.IsNotFound(err):
		// Request object not found, could have been deleted after reconcile request.
		// Owned objects are automatically garbage collected.
		// For additional cleanup logic use operatorUninstall function.
		// Return and don't requeue
		if upgrade.HasDeleteConfigMap(ctx, r.Client) {
			if uninstallErr := upgrade.OperatorUninstall(ctx, r.Client, cluster.GetRelease().Name); uninstallErr != nil {
				return ctrl.Result{}, fmt.Errorf("error while operator uninstall: %w", uninstallErr)
			}
		}

		return ctrl.Result{}, nil
	case err != nil:
		return ctrl.Result{}, err
	}

	// We don't need finalizer anymore, remove it if present to handle the
	// upgrade case
	if controllerutil.RemoveFinalizer(instance, finalizerName) {
		if err := r.Client.Update(ctx, instance); err != nil {
			return ctrl.Result{}, err
		}
	}

	// If DSC CR exist and deletion CM exist
	// delete DSC CR and let reconcile requeue
	if upgrade.HasDeleteConfigMap(ctx, r.Client) {
		err := r.Client.Delete(ctx, instance, client.PropagationPolicy(metav1.DeletePropagationForeground))
		if err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}

		return ctrl.Result{}, nil
	}

	if !instance.ObjectMeta.DeletionTimestamp.IsZero() {
		log.Info("Finalization DataScienceCluster start deleting instance", "name", instance.Name)

		if upgrade.HasDeleteConfigMap(ctx, r.Client) {
			return ctrl.Result{Requeue: true}, nil
		}

		return ctrl.Result{}, nil
	}

	// validate pre-requisites
	if err := r.validate(ctx, instance); err != nil {
		log.Info(err.Error())
		status.SetCondition(&instance.Status.Conditions, "Degraded", status.ReconcileFailed, err.Error(), corev1.ConditionTrue)
	}

	// deploy components
	if err := r.reconcileComponents(ctx, instance); err != nil {
		log.Info(err.Error())
		status.SetCondition(&instance.Status.Conditions, "Degraded", status.ReconcileFailed, err.Error(), corev1.ConditionTrue)
	}

	// keep conditions sorted
	slices.SortFunc(instance.Status.Conditions, func(a, b conditionsv1.Condition) int {
		return strings.Compare(string(a.Type), string(b.Type))
	})

	err = r.Client.ApplyStatus(ctx, instance, client.FieldOwner(fieldOwner), client.ForceOwnership)
	switch {
	case err == nil:
		return ctrl.Result{}, nil
	case k8serr.IsNotFound(err):
		return ctrl.Result{}, nil
	default:
		r.reportError(ctx, err, instance, "failed to update DataScienceCluster status")
		return ctrl.Result{}, err
	}
}

func (r *DataScienceClusterReconciler) validate(ctx context.Context, _ *dscv1.DataScienceCluster) error {
	// This case should not happen, since there is a webhook that blocks the creation
	// of more than one instance of the DataScienceCluster, however one can create a
	// DataScienceCluster instance while the operator is stopped, hence this extra check

	dscInstances := &dscv1.DataScienceClusterList{}
	if err := r.Client.List(ctx, dscInstances); err != nil {
		return fmt.Errorf("failed to retrieve DataScienceCluster resource: %w", err)
	}

	if len(dscInstances.Items) != 1 {
		return fmt.Errorf("failed to get a valid DataScienceCluster instance, expected to find 1 instance, found %d", len(dscInstances.Items))
	}

	dsciInstances := &dsciv1.DSCInitializationList{}
	err := r.Client.List(ctx, dsciInstances)
	if err != nil {
		return fmt.Errorf("failed to retrieve DSCInitialization resource: %w", err)
	}

	if len(dsciInstances.Items) != 1 {
		return fmt.Errorf("failed to get a valid DSCInitialization instance, expected to find 1 instance, found %d", len(dscInstances.Items))
	}

	return nil
}

func (r *DataScienceClusterReconciler) reconcileComponents(ctx context.Context, instance *dscv1.DataScienceCluster) error {
	log := logf.FromContext(ctx).WithName("DataScienceCluster")

	notReadyComponents := make([]string, 0)

	// all DSC defined components
	componentErrors := cr.ForEach(func(component cr.ComponentHandler) error {
		ci, err := r.reconcileComponent(ctx, instance, component)
		if err != nil {
			return err
		}

		if !cr.IsManaged(component, instance) {
			return nil
		}

		if !meta.IsStatusConditionTrue(ci.GetStatus().Conditions, status.ConditionTypeReady) {
			notReadyComponents = append(notReadyComponents, component.GetName())
		}

		return nil
	})

	// Process errors for components
	if componentErrors != nil {
		log.Info("DataScienceCluster Deployment Incomplete.")

		status.SetCompleteCondition(
			&instance.Status.Conditions,
			status.ReconcileCompletedWithComponentErrors,
			fmt.Sprintf("DataScienceCluster resource reconciled with component errors: %v", componentErrors),
		)

		r.Recorder.Eventf(instance, corev1.EventTypeNormal,
			"DataScienceClusterComponentFailures",
			"DataScienceCluster instance %s created, but have some failures in component %v", instance.Name, componentErrors)
	} else {
		log.Info("DataScienceCluster Deployment Completed.")

		// finalize reconciliation
		status.SetCompleteCondition(
			&instance.Status.Conditions,
			status.ReconcileCompleted,
			"DataScienceCluster resource reconciled successfully",
		)
	}

	if len(notReadyComponents) != 0 {
		instance.Status.Phase = status.PhaseNotReady

		conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    conditionsv1.ConditionType(status.ConditionTypeReady),
			Status:  corev1.ConditionFalse,
			Reason:  "NotReady",
			Message: fmt.Sprintf("Some components are not ready: %s", strings.Join(notReadyComponents, ",")),
		})
	} else {
		instance.Status.Phase = status.PhaseReady

		conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    conditionsv1.ConditionType(status.ConditionTypeReady),
			Status:  corev1.ConditionTrue,
			Reason:  "Ready",
			Message: "Ready",
		})
	}

	instance.Status.Release = cluster.GetRelease()
	instance.Status.ObservedGeneration = instance.Generation

	if componentErrors != nil {
		return componentErrors
	}

	return nil
}

func (r *DataScienceClusterReconciler) reconcileComponent(
	ctx context.Context,
	instance *dscv1.DataScienceCluster,
	component cr.ComponentHandler,
) (common.PlatformObject, error) {
	ms := component.GetManagementState(instance)
	componentCR := component.NewCRObject(instance)

	switch ms {
	case operatorv1.Managed:
		err := ctrl.SetControllerReference(instance, componentCR, r.Scheme)
		if err != nil {
			return nil, err
		}
		err = r.Client.Apply(ctx, componentCR, client.FieldOwner(fieldOwner), client.ForceOwnership)
		if err != nil {
			return nil, err
		}
	case operatorv1.Removed:
		err := r.Client.Delete(ctx, componentCR)
		if err != nil && !k8serr.IsNotFound(err) {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported management state: %s", ms)
	}

	if instance.Status.InstalledComponents == nil {
		instance.Status.InstalledComponents = make(map[string]bool)
	}

	err := component.UpdateDSCStatus(instance, componentCR)
	if err != nil {
		return nil, fmt.Errorf("failed to update status of DataScienceCluster component %s: %w", component.GetName(), err)
	}

	return componentCR, nil
}

func (r *DataScienceClusterReconciler) reportError(ctx context.Context, err error, instance *dscv1.DataScienceCluster, message string) {
	logf.FromContext(ctx).Error(err, message, "instance.Name", instance.Name)
	r.Recorder.Eventf(instance, corev1.EventTypeWarning, "DataScienceClusterReconcileError",
		"%s for instance %s", message, instance.Name)
}

// SetupWithManager sets up the controller with the Manager.
func (r *DataScienceClusterReconciler) SetupWithManager(_ context.Context, mgr ctrl.Manager) error {
	componentsPredicate := dependent.New(dependent.WithWatchStatus(true))

	return ctrl.NewControllerManagedBy(mgr).
		For(&dscv1.DataScienceCluster{}, builder.WithPredicates(predicates.DefaultPredicate)).
		// components
		Owns(&componentApi.Dashboard{}, builder.WithPredicates(componentsPredicate)).
		Owns(&componentApi.Workbenches{}, builder.WithPredicates(componentsPredicate)).
		Owns(&componentApi.Ray{}, builder.WithPredicates(componentsPredicate)).
		Owns(&componentApi.ModelRegistry{}, builder.WithPredicates(componentsPredicate)).
		Owns(&componentApi.TrustyAI{}, builder.WithPredicates(componentsPredicate)).
		Owns(&componentApi.Kueue{}, builder.WithPredicates(componentsPredicate)).
		Owns(&componentApi.CodeFlare{}, builder.WithPredicates(componentsPredicate)).
		Owns(&componentApi.TrainingOperator{}, builder.WithPredicates(componentsPredicate)).
		Owns(&componentApi.DataSciencePipelines{}, builder.WithPredicates(componentsPredicate)).
		Owns(&componentApi.Kserve{}, builder.WithPredicates(componentsPredicate)).
		Owns(&componentApi.ModelMeshServing{}, builder.WithPredicates(componentsPredicate)).
		Owns(&componentApi.ModelController{}, builder.WithPredicates(componentsPredicate)).
		// others
		Watches(
			&dsciv1.DSCInitialization{},
			handlers.Fn(r.watchDataScienceClusters)).
		Watches(
			&corev1.ConfigMap{},
			handlers.Fn(r.watchDataScienceClusters),
			builder.WithPredicates(r.filterDeleteConfigMap())).
		Complete(r)
}

func (r *DataScienceClusterReconciler) filterDeleteConfigMap() predicate.Funcs {
	filter := func(obj client.Object) bool {
		cm, ok := obj.(*corev1.ConfigMap)
		if !ok {
			return false
		}

		// Trigger reconcile function when uninstall configmap is created
		operatorNs, err := cluster.GetOperatorNamespace()
		if err != nil {
			return false
		}

		if cm.Namespace != operatorNs {
			return false
		}

		if cm.Labels[upgrade.DeleteConfigMapLabel] != "true" {
			return false
		}

		return true
	}

	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return filter(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return filter(e.ObjectNew)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
	}
}

func (r *DataScienceClusterReconciler) watchDataScienceClusters(ctx context.Context, _ client.Object) []reconcile.Request {
	instanceList := &dscv1.DataScienceClusterList{}
	err := r.Client.List(ctx, instanceList)
	if err != nil {
		return nil
	}

	requests := make([]reconcile.Request, len(instanceList.Items))
	for i := range instanceList.Items {
		requests[i] = reconcile.Request{NamespacedName: types.NamespacedName{Name: instanceList.Items[i].Name}}
	}

	return requests
}
