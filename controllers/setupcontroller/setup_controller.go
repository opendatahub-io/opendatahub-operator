package setupcontroller

import (
	"context"
	"fmt"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhClient "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	finalizerName = "datasciencecluster.opendatahub.io/finalizer"
)

type SetupControllerReconciler struct {
	*odhClient.Client
}

func (r *SetupControllerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithName("DataScienceCluster")
	log.Info("Reconciling setup controller", "Request.Name", req.Name)
	currentOperatorRelease := cluster.GetRelease()
	platform := currentOperatorRelease.Name
	instances := &dscv1.DataScienceClusterList{}

	// Check if the delete ConfigMap exists
	if !upgrade.HasDeleteConfigMap(ctx, r.Client) {
		log.Info("No delete ConfigMap found, skipping reconciliation")
		return reconcile.Result{}, nil
	}

	// Fetch the DataScienceCluster instance
	if len(instances.Items) == 0 {
		log.Info("No DataScienceCluster resources found")
		if upgrade.HasDeleteConfigMap(ctx, r.Client) {
			if uninstallErr := upgrade.OperatorUninstall(ctx, r.Client, platform); uninstallErr != nil {
				return ctrl.Result{}, fmt.Errorf("error while operator uninstall: %w", uninstallErr)
			}
		}
	}
	instance := &instances.Items[0]
	if upgrade.HasDeleteConfigMap(ctx, r.Client) {
		if controllerutil.ContainsFinalizer(instance, finalizerName) {
			if controllerutil.RemoveFinalizer(instance, finalizerName) {
				if err := r.Update(ctx, instance); err != nil {
					log.Info("Error to remove DSC finalizer", "error", err)
					return ctrl.Result{}, err
				}
				log.Info("Removed finalizer for DataScienceCluster", "name", instance.Name, "finalizer", finalizerName)

			}
		}
		// Delete the DataScienceCluster instance
		if err := r.Client.Delete(ctx, instance, []client.DeleteOption{}...); err != nil {
			if !k8serr.IsNotFound(err) {
				log.Error(err, "Failed to delete DataScienceCluster instance", "name", instance.Name)
				return reconcile.Result{}, err
			}
		}
		log.Info("DataScienceCluster instance deleted successfully, should be requeuing [?]")
		return reconcile.Result{Requeue: true}, nil
	}
	return ctrl.Result{}, nil
}

func (r *SetupControllerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dscv1.DataScienceCluster{}).
		Complete(r)
}
