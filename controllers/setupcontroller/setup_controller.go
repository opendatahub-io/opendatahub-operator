package setupcontroller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhClient "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
)

type SetupControllerReconciler struct {
	*odhClient.Client
}

const (
	finalizerName = "datasciencecluster.opendatahub.io/finalizer"
)

func (r *SetupControllerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithName("SetupContoller")
	log.Info("Reconciling setup controller", "Request.Name", req.Name) // log.V(1).Info(...)

	if !upgrade.HasDeleteConfigMap(ctx, r.Client) {
		return ctrl.Result{}, nil
	}

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
				log.Info("TRYING TO OperatorUninstall", "InstanceName", instance.Name)
				return ctrl.Result{}, fmt.Errorf("error while operator uninstall: %w", uninstallErr)
			}
		}

		return ctrl.Result{}, nil
	case err != nil:
		return ctrl.Result{}, err
	}

	if controllerutil.RemoveFinalizer(instance, finalizerName) {
		if err := r.Client.Update(ctx, instance); err != nil {
			return ctrl.Result{}, err
		}
	}

	// If DSC CR exist and deletion CM exist
	// delete DSC CR first then Operator Uninstall

	if upgrade.HasDeleteConfigMap(ctx, r.Client) {
		deleteErr := r.Client.Delete(ctx, instance, client.PropagationPolicy(metav1.DeletePropagationForeground))
		log.Info("TRYING TO DELETE", "InstanceName", instance.Name)
		if deleteErr != nil {
			return ctrl.Result{}, fmt.Errorf("error while deleting DSC CR: %w", deleteErr)
		}

		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

func (r *SetupControllerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.ConfigMap{}, builder.WithPredicates(r.filterDeleteConfigMap())).
		Complete(r)
}

func (r *SetupControllerReconciler) filterDeleteConfigMap() predicate.Funcs {
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
