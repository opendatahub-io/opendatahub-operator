package reconciler

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/exp/slices"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/opendatahub-io/opendatahub-operator/v2/apis/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/component"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/generation"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

var (
	// defaultPredicate is the default set of predicates associated to
	// resources when there is no specific predicate configured via the
	// builder.
	//
	// It would trigger a reconciliation if either the generation or
	// metadata (labels, annotations) have changed.
	defaultPredicate = predicate.Or(
		generation.New(),
		predicate.LabelChangedPredicate{},
		predicate.AnnotationChangedPredicate{},
	)
)

type forInput struct {
	object  components.ComponentObject
	options []builder.ForOption
}

type watchInput struct {
	object       client.Object
	eventHandler handler.EventHandler
	predicates   []predicate.Predicate
	owned        bool
	dynamic      bool
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

func Dynamic() WatchOpts {
	return func(a *watchInput) {
		a.dynamic = true
	}
}

type ComponentReconcilerBuilder struct {
	mgr           ctrl.Manager
	input         forInput
	watches       []watchInput
	predicates    []predicate.Predicate
	ownerName     string
	componentName string
	actions       []actions.Fn
	finalizers    []actions.Fn
}

func ComponentReconcilerFor(mgr ctrl.Manager, ownerName string, object components.ComponentObject, opts ...builder.ForOption) *ComponentReconcilerBuilder {
	crb := ComponentReconcilerBuilder{
		mgr:       mgr,
		ownerName: ownerName,
		input: forInput{
			object:  object,
			options: slices.Clone(opts),
		},
	}

	return &crb
}

func (b *ComponentReconcilerBuilder) WithComponentName(componentName string) *ComponentReconcilerBuilder {
	b.componentName = componentName
	return b
}

func (b *ComponentReconcilerBuilder) WithAction(value actions.Fn) *ComponentReconcilerBuilder {
	b.actions = append(b.actions, value)
	return b
}

func (b *ComponentReconcilerBuilder) WithFinalizer(value actions.Fn) *ComponentReconcilerBuilder {
	b.finalizers = append(b.finalizers, value)
	return b
}

func (b *ComponentReconcilerBuilder) Watches(object client.Object, opts ...WatchOpts) *ComponentReconcilerBuilder {
	in := watchInput{}
	in.object = object
	in.owned = false

	for _, opt := range opts {
		opt(&in)
	}

	if in.eventHandler == nil {
		// use the components.opendatahub.io/part-of label to find out
		// the owner
		in.eventHandler = handlers.LabelToName(labels.ComponentPartOf)
	}

	if len(in.predicates) == 0 {
		in.predicates = append(in.predicates, predicate.And(
			defaultPredicate,
			// use the components.opendatahub.io/part-of label to filter
			// events not related to the owner
			component.ForLabel(labels.ComponentPartOf, b.ownerName),
		))
	}

	b.watches = append(b.watches, in)

	return b
}

func (b *ComponentReconcilerBuilder) WatchesGVK(gvk schema.GroupVersionKind, opts ...WatchOpts) *ComponentReconcilerBuilder {
	return b.Watches(resources.GvkToUnstructured(gvk), opts...)
}

func (b *ComponentReconcilerBuilder) Owns(object client.Object, opts ...WatchOpts) *ComponentReconcilerBuilder {
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
		in.predicates = append(in.predicates, defaultPredicate)
	}

	b.watches = append(b.watches, in)

	return b
}

func (b *ComponentReconcilerBuilder) OwnsGVK(gvk schema.GroupVersionKind, opts ...WatchOpts) *ComponentReconcilerBuilder {
	return b.Owns(resources.GvkToUnstructured(gvk), opts...)
}

func (b *ComponentReconcilerBuilder) WithEventFilter(p predicate.Predicate) *ComponentReconcilerBuilder {
	b.predicates = append(b.predicates, p)
	return b
}

func (b *ComponentReconcilerBuilder) Build(_ context.Context) (*ComponentReconciler, error) {
	name := b.componentName
	if name == "" {
		kinds, _, err := b.mgr.GetScheme().ObjectKinds(b.input.object)
		if err != nil {
			return nil, err
		}
		if len(kinds) != 1 {
			return nil, fmt.Errorf("expected exactly one kind of object, got %d", len(kinds))
		}

		name = kinds[0].Kind
		name = strings.ToLower(name)
	}

	r, err := NewComponentReconciler(b.mgr, name, b.ownerName, b.input.object)
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
	r.AddAction(newDynamicWatchAction(b.mgr, cc, b.watches))

	return r, nil
}
