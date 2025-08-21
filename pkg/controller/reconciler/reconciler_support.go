// Package reconciler provides a fluent API for building Kubernetes controllers with support
// for both static and dynamic predicates. Dynamic predicates enable conditional watching
// based on runtime state rather than static configuration.
//
// Key Features:
// - Dynamic predicates that are evaluated at runtime during reconciliation
// - Thread-safe predicate storage with read-write locks
// - Fluent API for building reconcilers with watches, predicates, actions, and finalizers
// - Support for conditional watching based on CRD existence and other runtime conditions
// - Configurable predicate behavior with restrictive default (generation changes only)
// - Opt-in broader predicate behavior for label and annotation changes
//
// Example Usage:
//
//	// Default behavior - only generation changes trigger reconciliation
//	builder := ReconcilerFor(mgr, &MyObject{})
//	builder.Watches(
//	    &SomeResource{},
//	    Dynamic(CrdExists(schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "MyCRD"})),
//	)
//	reconciler, err := builder.Build(ctx)
//
//	// Opt-in broader behavior - generation, label, and annotation changes trigger reconciliation
//	builder := ReconcilerFor(mgr, &MyObject{}).WithBroadPredicate()
//	reconciler, err := builder.Build(ctx)
package reconciler

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/hashicorp/go-multierror"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/component"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// errDynamicWatchesNotImplemented is a sentinel error returned when dynamic watches are not yet supported.
var errDynamicWatchesNotImplemented = errors.New("dynamic watches are not yet implemented (pending controller-runtime support)")

type forInput struct {
	object  client.Object
	options []builder.ForOption
	gvk     schema.GroupVersionKind
}

// DynamicPredicate is a function type that evaluates whether a watch should be active
// based on the current reconciliation context. It receives the context and reconciliation
// request, and returns true if the watch should be active, false otherwise.
// Dynamic predicates are evaluated at runtime during each reconciliation cycle.
type DynamicPredicate func(context.Context, *types.ReconciliationRequest) bool

// watchInput contains the configuration for a single watch operation.
// It includes both static predicates (evaluated at watch creation time) and
// dynamic predicates (evaluated at runtime during reconciliation).
type watchInput struct {
	object            client.Object
	eventHandler      handler.EventHandler
	predicates        []predicate.Predicate
	dynamicPredicates []DynamicPredicate
	owned             bool
	dynamic           bool
}

type WatchOpts func(*watchInput)

func WithPredicates(values ...predicate.Predicate) WatchOpts {
	return func(a *watchInput) {
		a.predicates = append(a.predicates, values...)
	}
}

func WithEventHandler(value handler.EventHandler) WatchOpts {
	return func(a *watchInput) {
		a.eventHandler = value
	}
}

func WithEventMapper(value handler.MapFunc) WatchOpts {
	return func(a *watchInput) {
		a.eventHandler = handler.EnqueueRequestsFromMapFunc(value)
	}
}

// Dynamic creates a WatchOpts function that marks a watch as dynamic and stores
// the provided dynamic predicates for runtime evaluation. Dynamic predicates are
// evaluated during reconciliation to determine if a watch should be active.
// This enables conditional watching based on runtime state rather than static configuration.
func Dynamic(predicates ...DynamicPredicate) WatchOpts {
	return func(a *watchInput) {
		a.dynamic = true
		a.dynamicPredicates = append(a.dynamicPredicates, predicates...)
	}
}

// CrdExists is a DynamicPredicate that checks if a given CRD identified by its GVK exists.
// This is useful for conditionally watching resources that depend on CRDs being installed.
// Example usage:
//
//	builder.Watches(object, Dynamic(CrdExists(schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "MyCRD"})))
func CrdExists(crdGvk schema.GroupVersionKind) DynamicPredicate {
	return func(ctx context.Context, request *types.ReconciliationRequest) bool {
		if hasCrd, err := cluster.HasCRD(ctx, request.Client, crdGvk); err != nil {
			return false
		} else {
			return hasCrd
		}
	}
}

// ReconcilerBuilder provides a fluent API for building reconcilers with watches,
// predicates, actions, and finalizers. It supports both static and dynamic predicates
// for flexible watch configuration.
type ReconcilerBuilder[T common.PlatformObject] struct {
	mgr                 ctrl.Manager
	input               forInput
	watches             []watchInput
	predicates          []predicate.Predicate
	instanceName        string
	actions             []actions.Fn
	finalizers          []actions.Fn
	errors              error
	happyCondition      string
	dependantConditions []string
	// Cached clients to avoid recreation
	discoveryClient   discovery.DiscoveryInterface
	dynamicClient     dynamic.Interface
	dynamicPredicates []DynamicPredicate
	mu                sync.RWMutex
	// Control predicate behavior - when true, uses broader predicate including label/annotation changes
	useBroadPredicate bool
}

func ReconcilerFor[T common.PlatformObject](mgr ctrl.Manager, object T, opts ...builder.ForOption) *ReconcilerBuilder[T] {
	crb := ReconcilerBuilder[T]{
		mgr:                 mgr,
		happyCondition:      status.ConditionTypeReady,
		dependantConditions: []string{status.ConditionTypeProvisioningSucceeded},
	}

	gvk, err := mgr.GetClient().GroupVersionKindFor(object)
	if err != nil {
		crb.errors = multierror.Append(crb.errors, fmt.Errorf("unable to determine GVK: %w", err))
	}

	iops := slices.Clone(opts)
	if len(iops) == 0 {
		iops = append(iops, builder.WithPredicates(
			predicates.DefaultPredicate),
		)
	}

	crb.input = forInput{
		object:  object,
		options: iops,
		gvk:     gvk,
	}

	return &crb
}

func (b *ReconcilerBuilder[T]) WithConditions(dependants ...string) *ReconcilerBuilder[T] {
	b.dependantConditions = append(b.dependantConditions, dependants...)
	return b
}

func (b *ReconcilerBuilder[T]) WithInstanceName(instanceName string) *ReconcilerBuilder[T] {
	b.instanceName = instanceName
	return b
}

// WithBroadPredicate enables the broader predicate behavior that includes
// label and annotation changes in addition to generation changes.
// By default, only generation changes trigger reconciliation to reduce
// reconciliation frequency. Use this method to opt into the broader behavior
// when label or annotation changes should trigger reconciliation.
func (b *ReconcilerBuilder[T]) WithBroadPredicate() *ReconcilerBuilder[T] {
	b.useBroadPredicate = true
	return b
}

func (b *ReconcilerBuilder[T]) WithAction(value actions.Fn) *ReconcilerBuilder[T] {
	b.actions = append(b.actions, value)
	return b
}

func (b *ReconcilerBuilder[T]) WithFinalizer(value actions.Fn) *ReconcilerBuilder[T] {
	b.finalizers = append(b.finalizers, value)
	return b
}

func (b *ReconcilerBuilder[T]) Watches(object client.Object, opts ...WatchOpts) *ReconcilerBuilder[T] {
	in := watchInput{}
	in.object = object
	in.owned = false

	for _, opt := range opts {
		opt(&in)
	}

	if in.eventHandler == nil {
		// use the platform.opendatahub.io/instance.name label to find out
		// the owner
		in.eventHandler = handlers.AnnotationToName(annotations.InstanceName)
	}

	if len(in.predicates) == 0 {
		in.predicates = append(in.predicates, predicate.And(
			predicates.DefaultPredicate,
			// use the platform.opendatahub.io/part-of label to filter
			// events not related to the owner type
			component.ForLabel(labels.PlatformPartOf, strings.ToLower(b.input.gvk.Kind)),
		))
	}

	b.watches = append(b.watches, in)

	return b
}

func (b *ReconcilerBuilder[T]) WatchesGVK(gvk schema.GroupVersionKind, opts ...WatchOpts) *ReconcilerBuilder[T] {
	return b.Watches(resources.GvkToUnstructured(gvk), opts...)
}

func (b *ReconcilerBuilder[T]) Owns(object client.Object, opts ...WatchOpts) *ReconcilerBuilder[T] {
	in := watchInput{}
	in.object = object
	in.owned = true

	for _, opt := range opts {
		opt(&in)
	}

	if in.eventHandler == nil {
		in.eventHandler = handler.EnqueueRequestForOwner(
			b.mgr.GetScheme(),
			b.mgr.GetRESTMapper(),
			b.input.object,
			handler.OnlyControllerOwner(),
		)
	}

	if len(in.predicates) == 0 {
		in.predicates = append(in.predicates, predicates.DefaultPredicate)
	}

	b.watches = append(b.watches, in)

	return b
}

func (b *ReconcilerBuilder[T]) WithEventFilter(p predicate.Predicate) *ReconcilerBuilder[T] {
	b.predicates = append(b.predicates, p)
	return b
}

// AddDynamicPredicate adds a dynamic predicate to the reconciler builder.
// Dynamic predicates are evaluated at runtime during reconciliation and can be used
// to conditionally enable or disable watches based on the current state.
// This method is thread-safe and can be called concurrently.
func (b *ReconcilerBuilder[T]) AddDynamicPredicate(predicate DynamicPredicate) *ReconcilerBuilder[T] {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.dynamicPredicates = append(b.dynamicPredicates, predicate)
	return b
}

// GetDynamicPredicates returns a copy of all registered dynamic predicates.
// This method is thread-safe and can be called concurrently.
func (b *ReconcilerBuilder[T]) GetDynamicPredicates() []DynamicPredicate {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return slices.Clone(b.dynamicPredicates)
}

// ClearDynamicPredicates removes all registered dynamic predicates.
// This method is thread-safe and can be called concurrently.
func (b *ReconcilerBuilder[T]) ClearDynamicPredicates() *ReconcilerBuilder[T] {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.dynamicPredicates = nil
	return b
}

func (b *ReconcilerBuilder[T]) OwnsGVK(gvk schema.GroupVersionKind, opts ...WatchOpts) *ReconcilerBuilder[T] {
	return b.Owns(resources.GvkToUnstructured(gvk), opts...)
}

func (b *ReconcilerBuilder[T]) Build(_ context.Context) (*Reconciler, error) {
	if b.errors != nil {
		return nil, b.errors
	}

	// Validate manager configuration and initialize cached clients early
	// This ensures clients are available for all subsequent operations
	// Note: This initialization is single-threaded and should be called before any consumers run
	if err := b.validateManager(); err != nil {
		return nil, fmt.Errorf("invalid manager configuration: %w", err)
	}

	name := b.getInstanceName()
	obj, err := b.validateObject()
	if err != nil {
		return nil, err
	}

	r, err := b.createReconciler(name, obj)
	if err != nil {
		return nil, err
	}

	c, err := b.buildController(name)
	if err != nil {
		return nil, err
	}

	if err := b.setupWatches(c, r); err != nil {
		return nil, err
	}

	b.addEventFilters(c)
	b.addActionsAndFinalizers(r)

	_, err = c.Build(r)
	if err != nil {
		return nil, err
	}

	b.addDynamicWatchAction(r)

	return r, nil
}

func (b *ReconcilerBuilder[T]) validateManager() error {
	// Return early if clients are already initialized
	if b.discoveryClient != nil && b.dynamicClient != nil {
		return nil
	}

	// Get config once and reuse for both clients
	config := b.mgr.GetConfig()

	// Create discovery client first
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create discovery client: %w", err)
	}

	// Create dynamic client
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// Only assign clients to builder if both creations succeed
	b.discoveryClient = discoveryClient
	b.dynamicClient = dynamicClient

	return nil
}

func (b *ReconcilerBuilder[T]) getInstanceName() string {
	if b.instanceName != "" {
		return b.instanceName
	}
	return strings.ToLower(b.input.gvk.Kind)
}

func (b *ReconcilerBuilder[T]) validateObject() (T, error) {
	obj, ok := b.input.object.(T)
	if !ok {
		return obj, errors.New("invalid type for object")
	}
	return obj, nil
}

func (b *ReconcilerBuilder[T]) createReconciler(name string, obj T) (*Reconciler, error) {
	// Use cached clients that were initialized during validateManager
	r, err := newReconcilerWithClients(b.mgr, name, obj, b.discoveryClient, b.dynamicClient, WithConditionsManagerFactory(b.happyCondition, b.dependantConditions...))
	if err != nil {
		return nil, fmt.Errorf("failed to create reconciler for component %s: %w", name, err)
	}
	return r, nil
}

func (b *ReconcilerBuilder[T]) buildController(name string) (*builder.Builder, error) {
	c := ctrl.NewControllerManagedBy(b.mgr).Named(name)

	forOpts := b.getForOptions()
	c = c.For(b.input.object, forOpts...)

	return c, nil
}

// getForOptions provides default watch options when none are specified.
// By default, only GenerationChangedPredicate is used to reduce reconciliation frequency.
// Use WithBroadPredicate() to opt into broader behavior that includes label and annotation changes.
func (b *ReconcilerBuilder[T]) getForOptions() []builder.ForOption {
	forOpts := slices.Clone(b.input.options)
	if len(forOpts) == 0 {
		if b.useBroadPredicate {
			// Opt-in broader predicate that includes label and annotation changes
			forOpts = append(forOpts, builder.WithPredicates(predicate.Or(
				predicate.GenerationChangedPredicate{},
				predicate.LabelChangedPredicate{},
				predicate.AnnotationChangedPredicate{},
			)))
		} else {
			// Default restrictive predicate - only generation changes
			forOpts = append(forOpts, builder.WithPredicates(predicate.GenerationChangedPredicate{}))
		}
	}
	return forOpts
}

func (b *ReconcilerBuilder[T]) setupWatches(c *builder.Builder, r *Reconciler) error {
	for i := range b.watches {
		if err := b.processWatch(&b.watches[i], c, r); err != nil {
			return err
		}
	}
	return nil
}

func (b *ReconcilerBuilder[T]) processWatch(watch *watchInput, c *builder.Builder, r *Reconciler) error {
	if watch.owned {
		if err := b.addOwnedTypes(watch, r); err != nil {
			return err
		}
	}

	if watch.dynamic {
		// Extract identifying information for debug logging
		var watchName, resourceKind, namespace string

		// Get GVK information
		if kinds, _, err := b.mgr.GetScheme().ObjectKinds(watch.object); err == nil && len(kinds) > 0 {
			resourceKind = kinds[0].String()
		} else {
			resourceKind = "unknown"
		}

		// Get namespace information
		if watch.object != nil {
			namespace = watch.object.GetNamespace()
			if namespace == "" {
				namespace = "cluster-scoped"
			}
		} else {
			namespace = "unknown"
		}

		// Generate a watch identifier
		if watch.object != nil {
			watchName = fmt.Sprintf("%s/%s", resourceKind, watch.object.GetName())
		} else {
			watchName = resourceKind
		}

		r.Log.V(1).Info("Dynamic watch configured but being skipped",
			"watchName", watchName,
			"resourceKind", resourceKind,
			"namespace", namespace,
			"reason", "dynamic watches not yet implemented")

		return nil // Skip dynamic watches for now
	}

	c.Watches(
		watch.object,
		watch.eventHandler,
		builder.WithPredicates(watch.predicates...),
	)
	return nil
}

func (b *ReconcilerBuilder[T]) addOwnedTypes(watch *watchInput, r *Reconciler) error {
	kinds, _, err := b.mgr.GetScheme().ObjectKinds(watch.object)
	if err != nil {
		return err
	}

	for _, kind := range kinds {
		r.AddOwnedType(kind)
	}
	return nil
}

func (b *ReconcilerBuilder[T]) addEventFilters(c *builder.Builder) {
	for _, p := range b.predicates {
		c.WithEventFilter(p)
	}
}

func (b *ReconcilerBuilder[T]) addActionsAndFinalizers(r *Reconciler) {
	for _, action := range b.actions {
		r.AddAction(action)
	}
	for _, finalizer := range b.finalizers {
		r.AddFinalizer(finalizer)
	}
}

func (b *ReconcilerBuilder[T]) addDynamicWatchAction(r *Reconciler) {
	// Only add dynamic watch action if there are dynamic watches configured
	hasDynamicWatches := false
	for _, watch := range b.watches {
		if watch.dynamic {
			hasDynamicWatches = true
			break
		}
	}

	if !hasDynamicWatches {
		return
	}

	r.AddAction(
		newDynamicWatchAction(
			func(obj client.Object, eventHandler handler.EventHandler, predicates ...predicate.Predicate) error {
				// For now, return an error indicating dynamic watches are not supported
				// This can be implemented later when the correct controller interface is available
				return errDynamicWatchesNotImplemented
			},
			b.watches,
		),
	)
}
