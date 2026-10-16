package setup

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"
)

type SetupControllerReconciler struct {
	client.Client
}

func (r *SetupControllerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithName("SetupController")
	log.Info("Reconciling setup controller")

	if !upgrade.HasDeleteConfigMap(ctx, r.Client) {
		return ctrl.Result{}, nil
	}

	if err := r.markUninstallInProgress(ctx); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to report uninstall in progress: %w", err)
	}

	if err := upgrade.OperatorUninstall(ctx, r.Client, cluster.GetRelease().Name); err != nil {
		return ctrl.Result{}, fmt.Errorf("operator uninstall failed : %w", err)
	}

	return ctrl.Result{}, nil
}

func (r *SetupControllerReconciler) markUninstallInProgress(ctx context.Context) error {
	dscList := &dscv2.DataScienceClusterList{}
	if err := r.List(ctx, dscList); err != nil {
		return fmt.Errorf("failed to list DataScienceCluster resources: %w", err)
	}

	for i := range dscList.Items {
		dsc := &dscList.Items[i]
		if _, err := status.UpdateWithRetry(ctx, r.Client, dsc, func(saved *dscv2.DataScienceCluster) {
			status.SetCondition(
				&saved.Status.Conditions,
				status.ConditionTypeReady,
				status.UninstallInProgressReason,
				status.UninstallInProgressMessage,
				metav1.ConditionFalse,
			)
			saved.Status.Phase = status.PhaseNotReady
		}); err != nil {
			return fmt.Errorf("failed to update DataScienceCluster %q status: %w", dsc.Name, err)
		}
	}

	dsciList := &dsciv2.DSCInitializationList{}
	if err := r.List(ctx, dsciList); err != nil {
		return fmt.Errorf("failed to list DSCInitialization resources: %w", err)
	}

	for i := range dsciList.Items {
		dsci := &dsciList.Items[i]
		if _, err := status.UpdateWithRetry(ctx, r.Client, dsci, func(saved *dsciv2.DSCInitialization) {
			status.SetProgressingCondition(
				&saved.Status.Conditions,
				status.UninstallInProgressReason,
				status.UninstallInProgressMessage,
			)
			status.SetCondition(
				&saved.Status.Conditions,
				status.ConditionTypeReady,
				status.UninstallInProgressReason,
				status.UninstallInProgressMessage,
				metav1.ConditionFalse,
			)
			saved.Status.Phase = status.PhaseNotReady
		}); err != nil {
			return fmt.Errorf("failed to update DSCInitialization %q status: %w", dsci.Name, err)
		}
	}

	return nil
}

func (r *SetupControllerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	operatorNs, err := cluster.GetOperatorNamespace()

	if err != nil {
		return fmt.Errorf("failed to get operator namespace: %w", err)
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(resources.GvkToUnstructured(gvk.ConfigMap), builder.WithPredicates(r.filterDeleteConfigMap(operatorNs))).
		Complete(r)
}

func (r *SetupControllerReconciler) filterDeleteConfigMap(operatorNs string) predicate.Funcs {
	filter := func(obj client.Object) bool {
		_, isCM := obj.(*corev1.ConfigMap)
		if !isCM && obj.GetObjectKind().GroupVersionKind() != gvk.ConfigMap {
			return false
		}

		if obj.GetNamespace() != operatorNs {
			return false
		}

		if obj.GetLabels()[upgrade.DeleteConfigMapLabel] != "true" {
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
