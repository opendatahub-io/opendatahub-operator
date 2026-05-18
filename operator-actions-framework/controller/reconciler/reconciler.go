package reconciler

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/opendatahub-io/operator-actions-framework/api"
	"github.com/opendatahub-io/operator-actions-framework/controller/actions"
	odherrors "github.com/opendatahub-io/operator-actions-framework/controller/actions/errors"
	"github.com/opendatahub-io/operator-actions-framework/controller/conditions"
	"github.com/opendatahub-io/operator-actions-framework/controller/types"
	"github.com/opendatahub-io/operator-actions-framework/resources"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	DefaultFinalizerName             = "platform.opendatahub.io/finalizer"
	DefaultHappyCondition            = "Ready"
	DefaultProvisioningConditionType = "ProvisioningSucceeded"
	DefaultPhaseReady                = "Ready"
	DefaultPhaseNotReady             = "Not Ready"
)

type gvkInfo struct {
	owned bool
}

type manifestsBasePathProvider interface {
	GetManifestsBasePath() string
}

func getManifestsBasePath(mgr manager.Manager) string {
	if p, ok := mgr.(manifestsBasePathProvider); ok {
		return p.GetManifestsBasePath()
	}
	return ""
}

type chartsBasePathProvider interface {
	GetChartsBasePath() string
}

func getChartsBasePath(mgr manager.Manager) string {
	if p, ok := mgr.(chartsBasePathProvider); ok {
		return p.GetChartsBasePath()
	}
	return ""
}

// PreApplyFn is a callback invoked before actions in apply().
// It returns true to stop reconciliation (skip actions).
type PreApplyFn func(ctx context.Context, rr *types.ReconciliationRequest) bool

type ReconcilerOpt func(*Reconciler)

func WithConditionsManagerFactory(happy string, dependents ...string) ReconcilerOpt {
	return func(reconciler *Reconciler) {
		reconciler.conditionsManagerFactory = func(accessor api.ConditionsAccessor) *conditions.Manager {
			return conditions.NewManager(accessor, happy, dependents...)
		}
	}
}

// WithRelease sets the release information for the reconciler.
func WithRelease(release api.Release) ReconcilerOpt {
	return func(reconciler *Reconciler) {
		reconciler.Release = release
	}
}

// WithFinalizerName sets the finalizer name used by the reconciler.
func WithFinalizerName(name string) ReconcilerOpt {
	return func(reconciler *Reconciler) {
		reconciler.finalizerName = name
	}
}

// WithProvisioningConditionType sets the condition type used for provisioning status.
func WithProvisioningConditionType(conditionType string) ReconcilerOpt {
	return func(reconciler *Reconciler) {
		reconciler.provisioningConditionType = conditionType
	}
}

// WithPhaseNames sets the phase names used for ready and not-ready states.
func WithPhaseNames(ready, notReady string) ReconcilerOpt {
	return func(reconciler *Reconciler) {
		reconciler.phaseReady = ready
		reconciler.phaseNotReady = notReady
	}
}

// WithPreApplyFn sets a callback that runs before actions in apply().
// If it returns true, reconciliation is stopped.
func WithPreApplyFn(fn PreApplyFn) ReconcilerOpt {
	return func(reconciler *Reconciler) {
		reconciler.preApplyFn = fn
	}
}

// WithPreApplyFailedReason sets the reason string used when pre-apply stops reconciliation.
func WithPreApplyFailedReason(reason string) ReconcilerOpt {
	return func(reconciler *Reconciler) {
		reconciler.preApplyFailedReason = reason
	}
}

func withSkipStatusConditions(pred func() bool) ReconcilerOpt {
	return func(reconciler *Reconciler) {
		reconciler.skipStatusConditionsFn = pred
	}
}

// WithSkipConditionCleanup disables automatic stale condition cleanup after reconciliation.
func WithSkipConditionCleanup() ReconcilerOpt {
	return func(reconciler *Reconciler) {
		reconciler.skipConditionCleanup = true
	}
}

// WithDynamicOwnership enables dynamic ownership mode for the reconciler.
func WithDynamicOwnership(opts ...DynamicOwnershipOption) ReconcilerOpt {
	return func(reconciler *Reconciler) {
		reconciler.dynamicOwnershipEnabled = true

		cfg := &dynamicOwnershipConfig{}
		for _, opt := range opts {
			opt(cfg)
		}
		for _, g := range cfg.excludeGVKs {
			reconciler.excludeFromDynamicOwnership[g] = struct{}{}
		}
	}
}

// Reconciler provides generic reconciliation functionality for operator objects.
type Reconciler struct {
	Client          client.Client
	discoveryClient discovery.DiscoveryInterface
	dynamicClient   dynamic.Interface

	Scheme            *runtime.Scheme
	Actions           []actions.Fn
	Finalizer         []actions.Fn
	Log               logr.Logger
	Controller        controller.Controller
	Recorder          events.EventRecorder
	Release           api.Release
	ManifestsBasePath string
	ChartsBasePath    string

	name                        string
	finalizerName               string
	provisioningConditionType   string
	preApplyFailedReason        string
	phaseReady                  string
	phaseNotReady               string
	preApplyFn                  PreApplyFn
	instanceFactory             func() (api.PlatformObject, error)
	conditionsManagerFactory    func(api.ConditionsAccessor) *conditions.Manager
	gvks                        map[schema.GroupVersionKind]gvkInfo
	dynamicGvks                 sync.Map
	dynamicOwnershipEnabled     bool
	excludeFromDynamicOwnership map[schema.GroupVersionKind]struct{}
	skipConditionCleanup        bool
	skipStatusConditionsFn      func() bool
}

// NewReconciler creates a new reconciler for the given type.
func NewReconciler[T api.PlatformObject](mgr manager.Manager, name string, object T, opts ...ReconcilerOpt) (*Reconciler, error) {
	discoveryCli, err := discovery.NewDiscoveryClientForConfig(mgr.GetConfig())
	if err != nil {
		return nil, fmt.Errorf("unable to construct a Discovery client: %w", err)
	}
	dynamicCli, err := dynamic.NewForConfig(mgr.GetConfig())
	if err != nil {
		return nil, fmt.Errorf("unable to construct a Dynamic client: %w", err)
	}

	cc := Reconciler{
		Client:            mgr.GetClient(),
		Scheme:            mgr.GetScheme(),
		Log:               ctrl.Log.WithName("controllers").WithName(name),
		Recorder:          mgr.GetEventRecorder(name),
		ManifestsBasePath: getManifestsBasePath(mgr),
		ChartsBasePath:    getChartsBasePath(mgr),

		name:                      name,
		finalizerName:             DefaultFinalizerName,
		provisioningConditionType: DefaultProvisioningConditionType,
		preApplyFailedReason:      "PreConditionFailed",
		phaseReady:                DefaultPhaseReady,
		phaseNotReady:             DefaultPhaseNotReady,
		instanceFactory: func() (api.PlatformObject, error) {
			t := reflect.TypeOf(object).Elem()
			res, ok := reflect.New(t).Interface().(T)
			if !ok {
				return res, fmt.Errorf("unable to construct instance of %v", t)
			}

			return res, nil
		},
		conditionsManagerFactory: func(accessor api.ConditionsAccessor) *conditions.Manager {
			return conditions.NewManager(accessor, DefaultHappyCondition)
		},
		gvks:                        make(map[schema.GroupVersionKind]gvkInfo),
		excludeFromDynamicOwnership: make(map[schema.GroupVersionKind]struct{}),
		dynamicClient:               dynamicCli,
		discoveryClient:             discoveryCli,
	}

	for _, opt := range opts {
		opt(&cc)
	}

	return &cc, nil
}

func (r *Reconciler) GetRelease() api.Release {
	return r.Release
}

func (r *Reconciler) GetLogger() logr.Logger {
	return r.Log
}

func (r *Reconciler) GetClient() client.Client {
	return r.Client
}

func (r *Reconciler) GetDiscoveryClient() discovery.DiscoveryInterface {
	return r.discoveryClient
}

func (r *Reconciler) GetDynamicClient() dynamic.Interface {
	return r.dynamicClient
}

func (r *Reconciler) AddOwnedType(gvk schema.GroupVersionKind) {
	r.gvks[gvk] = gvkInfo{
		owned: true,
	}
}

func (r *Reconciler) AddDynamicOwnedType(gvk schema.GroupVersionKind) {
	r.dynamicGvks.Store(gvk, struct{}{})
}

func (r *Reconciler) Owns(gvk schema.GroupVersionKind) bool {
	i, ok := r.gvks[gvk]
	if ok && i.owned {
		return true
	}
	_, ok = r.dynamicGvks.Load(gvk)
	return ok
}

func (r *Reconciler) IsDynamicOwnershipEnabled() bool {
	return r.dynamicOwnershipEnabled
}

func (r *Reconciler) IsExcludedFromDynamicOwnership(gvk schema.GroupVersionKind) bool {
	_, excluded := r.excludeFromDynamicOwnership[gvk]
	return excluded
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

	if err := r.Client.Get(ctx, req.NamespacedName, res); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if err := resources.EnsureGroupVersionKind(r.Client.Scheme(), res); err != nil {
		return ctrl.Result{}, fmt.Errorf("unable to set GVK to instance: %w", err)
	}

	if !res.GetDeletionTimestamp().IsZero() {
		if !controllerutil.ContainsFinalizer(res, r.finalizerName) {
			return ctrl.Result{}, nil
		}

		if err := r.delete(ctx, res); err != nil {
			return ctrl.Result{}, err
		}

		if err := r.removeFinalizer(ctx, res); err != nil {
			return ctrl.Result{}, err
		}
	} else {
		if err := r.addFinalizer(ctx, res); err != nil {
			return ctrl.Result{}, err
		}

		requeueAfter, err := r.apply(ctx, res)
		if err != nil {
			return ctrl.Result{}, err
		}

		if requeueAfter > 0 {
			l.V(1).Info("scheduling requeue for DAG timeout", "after", requeueAfter.Truncate(time.Second))
			return ctrl.Result{RequeueAfter: requeueAfter}, nil
		}
	}

	return ctrl.Result{}, nil
}

func (r *Reconciler) addFinalizer(ctx context.Context, res api.PlatformObject) error {
	if len(r.Finalizer) == 0 {
		return nil
	}

	if !controllerutil.AddFinalizer(res, r.finalizerName) {
		return nil
	}

	l := log.FromContext(ctx)
	l.Info("adding finalizer")
	if err := r.Client.Update(ctx, res); err != nil {
		return fmt.Errorf("failed to add finalizer %s to %s: %w", r.finalizerName, res.GetName(), err)
	}

	return nil
}

func (r *Reconciler) removeFinalizer(ctx context.Context, res api.PlatformObject) error {
	if !controllerutil.RemoveFinalizer(res, r.finalizerName) {
		return nil
	}

	l := log.FromContext(ctx)
	l.Info("removing finalizer")
	if err := r.Client.Update(ctx, res); err != nil {
		return fmt.Errorf("failed to remove finalizer %s from %s: %w", r.finalizerName, res.GetName(), err)
	}

	return nil
}

func (r *Reconciler) delete(ctx context.Context, res api.PlatformObject) error {
	l := log.FromContext(ctx)
	l.Info("delete")

	rr := types.ReconciliationRequest{
		Client:            r.Client,
		Controller:        r,
		Instance:          res,
		Conditions:        r.conditionsManagerFactory(res),
		Release:           r.Release,
		ManifestsBasePath: r.ManifestsBasePath,
		ChartsBasePath:    r.ChartsBasePath,

		Manifests: make([]types.ManifestInfo, 0),
	}

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

func (r *Reconciler) apply(ctx context.Context, res api.PlatformObject) (time.Duration, error) {
	l := log.FromContext(ctx)
	l.Info("apply")

	rr := types.ReconciliationRequest{
		Client:            r.Client,
		Controller:        r,
		Instance:          res,
		Conditions:        r.conditionsManagerFactory(res),
		Release:           r.Release,
		ManifestsBasePath: r.ManifestsBasePath,
		ChartsBasePath:    r.ChartsBasePath,

		Manifests: make([]types.ManifestInfo, 0),
	}

	rr.Conditions.Reset()

	shouldStop := false
	if r.preApplyFn != nil {
		shouldStop = r.preApplyFn(ctx, &rr)
	}

	var requeueAfter time.Duration
	var provisionErr error

	if shouldStop {
		l.Info("Pre-apply check not met, stopping reconciliation")

		rr.Conditions.MarkFalse(
			r.provisioningConditionType,
			conditions.WithReason(r.preApplyFailedReason),
			conditions.WithMessage("pre-conditions not met, see status conditions for details"),
			conditions.WithObservedGeneration(rr.Instance.GetGeneration()),
		)
	} else {
		for _, action := range r.Actions {
			l.Info("Executing action", "action", action)

			actx := log.IntoContext(
				ctx,
				l.WithName(actions.ActionGroup).WithName(action.String()),
			)

			provisionErr = action(actx, &rr)
			if provisionErr != nil {
				re := odherrors.RequeueAfterError{}
				if errors.As(provisionErr, &re) {
					requeueAfter = re.After
					provisionErr = nil

					continue
				}

				break
			}
		}

		if provisionErr != nil {
			rr.Conditions.MarkFalse(
				r.provisioningConditionType,
				conditions.WithError(provisionErr),
				conditions.WithObservedGeneration(rr.Instance.GetGeneration()),
			)
		} else {
			rr.Conditions.MarkTrue(
				r.provisioningConditionType,
				conditions.WithObservedGeneration(rr.Instance.GetGeneration()),
			)
		}
	}

	if !r.skipConditionCleanup {
		rr.Conditions.CleanupStaleConditions()
	}

	is := rr.Instance.GetStatus()
	is.Phase = r.phaseNotReady

	rr.Conditions.RecomputeHappiness("")

	rr.Conditions.Sort()

	if rr.Conditions.IsHappy() {
		is.Phase = r.phaseReady
		is.ObservedGeneration = rr.Instance.GetGeneration()
	}

	if r.skipStatusConditionsFn != nil && r.skipStatusConditionsFn() {
		is.Conditions = nil
		is.Phase = ""
		is.ObservedGeneration = 0
	}

	err := resources.ApplyStatus(
		ctx,
		r.Client,
		rr.Instance,
		client.FieldOwner(r.name),
		client.ForceOwnership,
	)

	if err != nil && !k8serr.IsNotFound(err) {
		r.Recorder.Eventf(
			res,
			nil,
			corev1.EventTypeNormal,
			"ReconcileError",
			"Reconcile",
			err.Error(),
		)

		return 0, fmt.Errorf("reconcile failed: %w", err)
	}

	if provisionErr != nil {
		se := odherrors.StopError{}
		if errors.As(provisionErr, &se) {
			return 0, nil
		}

		r.Recorder.Eventf(
			res,
			nil,
			corev1.EventTypeWarning,
			"ProvisioningError",
			"Provision",
			provisionErr.Error(),
		)

		return 0, fmt.Errorf("provisioning failed: %w", provisionErr)
	}

	return requeueAfter, nil
}
