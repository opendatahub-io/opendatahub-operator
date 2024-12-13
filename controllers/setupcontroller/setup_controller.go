package setupcontroller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhClient "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
)

type SetupControllerReconciler struct {
	*odhClient.Client
}

func (r *SetupControllerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithName("DataScienceCluster")
	log.Info("Reconciling setup controller", "Request.Name", req.Name)

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

	//upgrade
	if upgrade.HasDeleteConfigMap(ctx, r.Client) {
		err := r.Client.Delete(ctx, instance, client.PropagationPolicy(metav1.DeletePropagationForeground))
		if err != nil {
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}

		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

func (r *SetupControllerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.ConfigMap{}).
		Complete(r)
}
