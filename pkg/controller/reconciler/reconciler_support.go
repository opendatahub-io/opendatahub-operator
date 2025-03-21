package reconciler

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/go-multierror"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/component"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

type forInput struct {
	object  client.Object
	options []builder.ForOption
	gvk     schema.GroupVersionKind
}

type DynamicPredicate func(context.Context, *types.ReconciliationRequest) bool

type watchInput struct {
	object       client.Object
	eventHandler handler.EventHandler
	predicates   []predicate.Predicate
	owned        bool
	dynamic      bool
	dynamicPred  []DynamicPredicate
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

func Dynamic(predicates ...DynamicPredicate) WatchOpts {
	return func(a *watchInput) {
		a.dynamic = true
		a.dynamicPred = slices.Clone(predicates)
	}
}

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

func (b *ReconcilerBuilder[T]) OwnsGVK(gvk schema.GroupVersionKind, opts ...WatchOpts) *ReconcilerBuilder[T] {
	return b.Owns(resources.GvkToUnstructured(gvk), opts...)
}

func (b *ReconcilerBuilder[T]) Build(_ context.Context) (*Reconciler, error) {
	if b.errors != nil {
		return nil, b.errors
	}
	name := b.instanceName
	if name == "" {
		name = strings.ToLower(b.input.gvk.Kind)
	}

	obj, ok := b.input.object.(T)
	if !ok {
		return nil, errors.New("invalid type for object")
	}

	r, err := NewReconciler(b.mgr, name, obj, WithConditionsManagerFactory(b.happyCondition, b.dependantConditions...))
	if err != nil {
		return nil, fmt.Errorf("failed to create reconciler for component %s: %w", name, err)
	}

	c := ctrl.NewControllerManagedBy(b.mgr)

	// automatically add default predicates to the watched API if no
	// predicates are provided
	forOpts := b.input.options
	if len(forOpts) == 0 {
		forOpts = append(forOpts, builder.WithPredicates(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.LabelChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
		)))
	}

	c = c.For(b.input.object, forOpts...)

	for i := range b.watches {
		if b.watches[i].owned {
			kinds, _, err := b.mgr.GetScheme().ObjectKinds(b.watches[i].object)
			if err != nil {
				return nil, err
			}

			for i := range kinds {
				r.AddOwnedType(kinds[i])
			}
		}

		// if the watch is dynamic, then the watcher will be registered
		// at later stage
		if b.watches[i].dynamic {
			continue
		}

		c = c.Watches(
			b.watches[i].object,
			b.watches[i].eventHandler,
			builder.WithPredicates(b.watches[i].predicates...),
		)
	}

	for i := range b.predicates {
		c = c.WithEventFilter(b.predicates[i])
	}

	for i := range b.actions {
		r.AddAction(b.actions[i])
	}
	for i := range b.finalizers {
		r.AddFinalizer(b.finalizers[i])
	}

	cc, err := c.Build(r)
	if err != nil {
		return nil, err
	}

	// internal action
	r.AddAction(
		newDynamicWatchAction(
			func(obj client.Object, eventHandler handler.EventHandler, predicates ...predicate.Predicate) error {
				return cc.Watch(source.Kind(b.mgr.GetCache(), obj), eventHandler, predicates...)
			},
			b.watches,
		),
	)

	return r, nil
}
