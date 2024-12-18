package reconciler

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/opendatahub-io/opendatahub-operator/v2/apis/common"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	odhClient "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
	odhManager "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/manager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// Reconciler provides generic reconciliation functionality for ODH objects.
type Reconciler[T common.PlatformObject] struct {
	Client     *odhClient.Client
	Scheme     *runtime.Scheme
	Actions    []actions.Fn
	Finalizer  []actions.Fn
	Log        logr.Logger
	Controller controller.Controller
	Recorder   record.EventRecorder
	Release    cluster.Release

	name            string
	m               *odhManager.Manager
	instanceFactory func() (T, error)
}

// NewReconciler creates a new reconciler for the given type.
func NewReconciler[T common.PlatformObject](mgr manager.Manager, name string, object T) (*Reconciler[T], error) {
	oc, err := odhClient.NewFromManager(mgr)
	if err != nil {
		return nil, err
	}

	cc := Reconciler[T]{
		Client:   oc,
		Scheme:   mgr.GetScheme(),
		Log:      ctrl.Log.WithName("controllers").WithName(name),
		Recorder: mgr.GetEventRecorderFor(name),
		Release:  cluster.GetRelease(),
		name:     name,
		m:        odhManager.New(mgr),
		instanceFactory: func() (T, error) {
			t := reflect.TypeOf(object).Elem()
			res, ok := reflect.New(t).Interface().(T)
			if !ok {
				return res, fmt.Errorf("unable to construct instance of %v", t)
			}

			return res, nil
		},
	}

	return &cc, nil
}

func (r *Reconciler[T]) GetRelease() cluster.Release {
	return r.Release
}

func (r *Reconciler[T]) GetLogger() logr.Logger {
	return r.Log
}

func (r *Reconciler[T]) AddOwnedType(gvk schema.GroupVersionKind) {
	r.m.AddGVK(gvk, true)
}

func (r *Reconciler[T]) Owns(obj client.Object) bool {
	return r.m.Owns(obj.GetObjectKind().GroupVersionKind())
}

func (r *Reconciler[T]) AddAction(action actions.Fn) {
	r.Actions = append(r.Actions, action)
}

func (r *Reconciler[T]) AddFinalizer(action actions.Fn) {
	r.Finalizer = append(r.Finalizer, action)
}

func (r *Reconciler[T]) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)
	l.Info("reconcile")

	res, err := r.instanceFactory()
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.Client.Get(ctx, client.ObjectKey{Name: req.Name}, res); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !res.GetDeletionTimestamp().IsZero() {
		if err := r.delete(ctx, res); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		if err := r.apply(ctx, res); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *Reconciler[T]) delete(ctx context.Context, res client.Object) error {
	l := log.FromContext(ctx)
	l.Info("delete")

	rr := types.ReconciliationRequest{
		Client:    r.Client,
		Manager:   r.m,
		Instance:  res,
		Release:   r.Release,
		Manifests: make([]types.ManifestInfo, 0),

		// The DSCI should not be required when deleting a component, if the
		// component requires some additional info, then such info should be
		// stored as part of the spec/status
		DSCI: nil,
	}

	// Execute finalizers
	for _, action := range r.Finalizer {
		l.V(3).Info("Executing finalizer", "action", action)

		actx := log.IntoContext(
			ctx,
			l.WithName(actions.ActionGroup).WithName(action.String()),
		)

		if err := action(actx, &rr); err != nil {
			se := odherrors.StopError{}
			if !errors.As(err, &se) {
				l.Error(err, "Failed to execute finalizer", "action", action)
				return err
			}

			l.V(3).Info("detected stop marker", "action", action)
			break
		}
	}

	return nil
}

func (r *Reconciler[T]) apply(ctx context.Context, res client.Object) error {
	l := log.FromContext(ctx)
	l.Info("apply")

	dscil := dsciv1.DSCInitializationList{}
	if err := r.Client.List(ctx, &dscil); err != nil {
		return err
	}

	if len(dscil.Items) != 1 {
		return errors.New("unable to find DSCInitialization")
	}

	rr := types.ReconciliationRequest{
		Client:    r.Client,
		Manager:   r.m,
		Instance:  res,
		DSCI:      &dscil.Items[0],
		Release:   r.Release,
		Manifests: make([]types.ManifestInfo, 0),
	}

	// Execute actions
	for _, action := range r.Actions {
		l.Info("Executing action", "action", action)

		actx := log.IntoContext(
			ctx,
			l.WithName(actions.ActionGroup).WithName(action.String()),
		)

		if err := action(actx, &rr); err != nil {
			se := odherrors.StopError{}
			if !errors.As(err, &se) {
				l.Error(err, "Failed to execute action", "action", action)
				return err
			}

			l.V(3).Info("detected stop marker", "action", action)
			break
		}
	}

	err := r.Client.ApplyStatus(
		ctx,
		rr.Instance,
		client.FieldOwner(r.name),
		client.ForceOwnership,
	)

	if err != nil {
		return client.IgnoreNotFound(err)
	}

	return nil
}
