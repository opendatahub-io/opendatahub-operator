package setupcontroller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhClient "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
)

type SetupControllerReconciler struct {
	*odhClient.Client
}

func (r *SetupControllerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithName("SetupController")
	log.Info("Reconciling setup controller")

	if !upgrade.HasDeleteConfigMap(ctx, r.Client) {
		return ctrl.Result{}, nil
	}

	if err := upgrade.OperatorUninstall(ctx, r.Client, cluster.GetRelease().Name); err != nil {
		return ctrl.Result{}, fmt.Errorf("operator uninstall failed : %w", err)
	}

	return ctrl.Result{}, nil
}

func (r *SetupControllerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	operatorNs, err := cluster.GetOperatorNamespace()

	if err != nil {
		return fmt.Errorf("failed to get operator namespace: %w", err)
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.ConfigMap{}, builder.WithPredicates(r.filterDeleteConfigMap(operatorNs))).
		Complete(r)
}

func (r *SetupControllerReconciler) filterDeleteConfigMap(operatorNs string) predicate.Funcs {
	filter := func(obj client.Object) bool {
		cm, ok := obj.(*corev1.ConfigMap)

		if !ok {
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
