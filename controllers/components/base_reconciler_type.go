package components

import (
	"context"
	"errors"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/opendatahub-io/opendatahub-operator/v2/apis/components"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"reflect"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type Action interface {
	Execute(ctx context.Context, rr *ReconciliationRequest) error
}

type BaseAction struct {
	Log logr.Logger
}

type ResourceObject interface {
	client.Object
	components.WithStatus
}

type ReconciliationRequest struct {
	client.Client

	Instance  client.Object
	DSC       *dscv1.DataScienceCluster
	DSCI      *dsciv1.DSCInitialization
	Platform  cluster.Platform
	Manifests Manifests
}

type Manifests struct {
	Paths map[string]string
}

type BaseReconciler[T ResourceObject] struct {
	Client     client.Client
	Scheme     *runtime.Scheme
	Actions    []Action
	Finalizer  []Action
	Log        logr.Logger
	Manager    manager.Manager
	Controller controller.Controller
	Recorder   record.EventRecorder
	Platform   cluster.Platform
}

func NewBaseReconciler[T ResourceObject](mgr manager.Manager, name string) *BaseReconciler[T] {
	return &BaseReconciler[T]{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Log:      ctrl.Log.WithName("controllers").WithName(name),
		Manager:  mgr,
		Recorder: mgr.GetEventRecorderFor(name),
		Platform: cluster.GetRelease().Name,
	}
}

func (r *BaseReconciler[T]) AddAction(action Action) {
	r.Actions = append(r.Actions, action)
}

func (r *BaseReconciler[T]) AddFinalizer(action Action) {
	r.Finalizer = append(r.Finalizer, action)
}

func (r *BaseReconciler[T]) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	res := reflect.New(reflect.TypeOf(*new(T)).Elem()).Interface().(T)
	if err := r.Client.Get(ctx, req.NamespacedName, res); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	dscl := dscv1.DataScienceClusterList{}
	if err := r.Client.List(ctx, &dscl); err != nil {
		return ctrl.Result{}, err
	}

	if len(dscl.Items) != 1 {
		return ctrl.Result{}, errors.New("unable to find DataScienceCluster")
	}

	dscil := dsciv1.DSCInitializationList{}
	if err := r.Client.List(ctx, &dscil); err != nil {
		return ctrl.Result{}, err
	}

	if len(dscil.Items) != 1 {
		return ctrl.Result{}, errors.New("unable to find DSCInitialization")
	}

	rr := ReconciliationRequest{
		Client:   r.Client,
		Instance: res,
		DSC:      &dscl.Items[0],
		DSCI:     &dscil.Items[0],
		Platform: r.Platform,
		Manifests: Manifests{
			Paths: make(map[string]string),
		},
	}

	// Handle deletion
	if !res.GetDeletionTimestamp().IsZero() {
		// Execute finalizers
		for _, action := range r.Finalizer {
			if err := action.Execute(ctx, &rr); err != nil {
				l.Error(err, "Failed to execute finalizer", "action", fmt.Sprintf("%T", action))
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{}, nil
	}

	// Execute actions
	for _, action := range r.Actions {
		if err := action.Execute(ctx, &rr); err != nil {
			l.Error(err, "Failed to execute action", "action", fmt.Sprintf("%T", action))
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}
