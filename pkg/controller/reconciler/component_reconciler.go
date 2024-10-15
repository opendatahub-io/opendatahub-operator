package types

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	odhClient "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

type ComponentReconciler[T types.ResourceObject] struct {
	Client     *odhClient.Client
	Scheme     *runtime.Scheme
	Actions    []actions.Action
	Finalizer  []actions.Action
	Log        logr.Logger
	Manager    manager.Manager
	Controller controller.Controller
	Recorder   record.EventRecorder
	Platform   cluster.Platform
}

func NewComponentReconciler[T types.ResourceObject](ctx context.Context, mgr manager.Manager, name string) (*ComponentReconciler[T], error) {
	oc, err := odhClient.NewFromManager(ctx, mgr)
	if err != nil {
		return nil, err
	}

	cc := ComponentReconciler[T]{
		Client:   oc,
		Scheme:   mgr.GetScheme(),
		Log:      ctrl.Log.WithName("controllers").WithName(name),
		Manager:  mgr,
		Recorder: mgr.GetEventRecorderFor(name),
		Platform: cluster.GetRelease().Name,
	}

	return &cc, nil
}

func (r *ComponentReconciler[T]) GetLogger() logr.Logger {
	return r.Log
}

func (r *ComponentReconciler[T]) AddAction(action actions.Action) {
	r.Actions = append(r.Actions, action)
}

func (r *ComponentReconciler[T]) AddFinalizer(action actions.Action) {
	r.Finalizer = append(r.Finalizer, action)
}

func (r *ComponentReconciler[T]) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	t := reflect.TypeOf(*new(T)).Elem()
	res, ok := reflect.New(t).Interface().(T)
	if !ok {
		return ctrl.Result{}, fmt.Errorf("unable to construct instance of %v", t)
	}
	if err := r.Client.Get(ctx, client.ObjectKey{Name: req.Name}, res); err != nil {
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

	rr := types.ReconciliationRequest{
		Client:    r.Client,
		Instance:  res,
		DSC:       &dscl.Items[0],
		DSCI:      &dscil.Items[0],
		Platform:  r.Platform,
		Manifests: make(map[cluster.Platform]string),
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

	// update status
	err := r.Client.ApplyStatus(
		ctx,
		rr.Instance,
		client.FieldOwner(rr.Instance.GetName()),
		client.ForceOwnership,
	)

	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
