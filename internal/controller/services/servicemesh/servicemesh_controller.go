package servicemesh

import (
	"context"
	"reflect"

	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
)

type ServiceMeshReconciler struct {
	Client   client.Client
	Recorder record.EventRecorder
}

func (r *ServiceMeshReconciler) dsciServiceMeshPredicate() predicate.Funcs {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldDsci, ok := e.ObjectOld.(*dsciv1.DSCInitialization)
			if !ok {
				return false
			}
			newDsci, ok := e.ObjectNew.(*dsciv1.DSCInitialization)
			if !ok {
				return false
			}

			return !reflect.DeepEqual(oldDsci.Spec.ServiceMesh, newDsci.Spec.ServiceMesh)
		},

		CreateFunc: func(e event.CreateEvent) bool {
			return true
		},

		DeleteFunc: func(e event.DeleteEvent) bool {
			return true
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceMeshReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	logf.FromContext(ctx).Info("Adding controller for ServiceMesh.")

	return ctrl.NewControllerManagedBy(mgr).
		Named("servicemesh-controller").
		For(&dsciv1.DSCInitialization{}, builder.WithPredicates(r.dsciServiceMeshPredicate())).
		Complete(r)
}

func (r *ServiceMeshReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("Reconciling ServiceMesh controller")

	dsciInstance := &dsciv1.DSCInitialization{}
	if err := r.Client.Get(ctx, req.NamespacedName, dsciInstance); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !dsciInstance.DeletionTimestamp.IsZero() {
		// DSCI is being deleted, remove ServiceMesh
		if err := r.removeServiceMesh(ctx, dsciInstance); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	// Apply Service Mesh configurations
	if err := r.configureServiceMesh(ctx, dsciInstance); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
