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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/opendatahub-io/opendatahub-operator/v2/apis/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	odhClient "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
	odhManager "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/manager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const platformFinalizer = "platform.opendatahub.io/finalizer"

// Reconciler provides generic reconciliation functionality for ODH objects.
type Reconciler struct {
	Client     *odhClient.Client
	Scheme     *runtime.Scheme
	Actions    []actions.Fn
	Finalizer  []actions.Fn
	Log        logr.Logger
	Controller controller.Controller
	Recorder   record.EventRecorder
	Release    common.Release

	name            string
	m               *odhManager.Manager
	instanceFactory func() (common.PlatformObject, error)
}

// NewReconciler creates a new reconciler for the given type.
func NewReconciler[T common.PlatformObject](mgr manager.Manager, name string, object T) (*Reconciler, error) {
	oc, err := odhClient.NewFromManager(mgr)
	if err != nil {
		return nil, err
	}

	cc := Reconciler{
		Client:   oc,
		Scheme:   mgr.GetScheme(),
		Log:      ctrl.Log.WithName("controllers").WithName(name),
		Recorder: mgr.GetEventRecorderFor(name),
		Release:  cluster.GetRelease(),
		name:     name,
		m:        odhManager.New(mgr),
		instanceFactory: func() (common.PlatformObject, error) {
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

func (r *Reconciler) GetRelease() common.Release {
	return r.Release
}

func (r *Reconciler) GetLogger() logr.Logger {
	return r.Log
}

func (r *Reconciler) AddOwnedType(gvk schema.GroupVersionKind) {
	r.m.AddGVK(gvk, true)
}

func (r *Reconciler) Owns(obj client.Object) bool {
	return r.m.Owns(obj.GetObjectKind().GroupVersionKind())
}

func (r *Reconciler) AddAction(action actions.Fn) {
	r.Actions = append(r.Actions, action)
}

func (r *Reconciler) AddFinalizer(action actions.Fn) {
	r.Finalizer = append(r.Finalizer, action)
}

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
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
		// resource is being deleted, attempt to perform clean-up logic and remove finalizer
		if !controllerutil.ContainsFinalizer(res, platformFinalizer) {
			return ctrl.Result{}, nil
		}

		if err := r.delete(ctx, res); err != nil {
			return ctrl.Result{}, err
		}

		if err := r.removeFinalizer(ctx, res); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		// resource is not being deleted, attempt to add finalizer
		if err := r.addFinalizer(ctx, res); err != nil {
			return ctrl.Result{}, err
		}

		if err := r.apply(ctx, res); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *Reconciler) addFinalizer(ctx context.Context, res common.PlatformObject) error {
	// no finalizer action present => no finalizer to be added/checked for
	if len(r.Finalizer) == 0 {
		return nil
	}

	if !controllerutil.AddFinalizer(res, platformFinalizer) {
		// finalizer already present
		return nil
	}

	l := log.FromContext(ctx)
	l.Info("adding finalizer")
	if err := r.Client.Update(ctx, res); err != nil {
		return fmt.Errorf("failed to add finalizer %s to %s: %w", platformFinalizer, res.GetName(), err)
	}

	return nil
}

func (r *Reconciler) removeFinalizer(ctx context.Context, res common.PlatformObject) error {
	if !controllerutil.RemoveFinalizer(res, platformFinalizer) {
		return nil
	}

	l := log.FromContext(ctx)
	l.Info("removing finalizer")
	if err := r.Client.Update(ctx, res); err != nil {
		return fmt.Errorf("failed to remove finalizer %s from %s: %w", platformFinalizer, res.GetName(), err)
	}

	return nil
}

func (r *Reconciler) delete(ctx context.Context, res common.PlatformObject) error {
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

func (r *Reconciler) apply(ctx context.Context, res common.PlatformObject) error {
	l := log.FromContext(ctx)
	l.Info("apply")

	dsci, err := cluster.GetDSCI(ctx, r.Client)
	if err != nil {
		return errors.New("unable to find a DSCInitialization instance")
	}

	rr := types.ReconciliationRequest{
		Client:    r.Client,
		Manager:   r.m,
		Instance:  res,
		DSCI:      dsci,
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

	err = r.Client.ApplyStatus(
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
