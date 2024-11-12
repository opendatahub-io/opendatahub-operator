package reconciler

import (
	"context"
	"fmt"
	"strings"

	"golang.org/x/exp/slices"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
)

type forInput struct {
	object  components.ComponentObject
	options []builder.ForOption
}

type watchInput struct {
	object       client.Object
	eventHandler handler.EventHandler
	options      []builder.WatchesOption
}
type ownInput struct {
	object  client.Object
	options []builder.OwnsOption
}

type ComponentReconcilerBuilder struct {
	mgr           ctrl.Manager
	input         forInput
	watches       []watchInput
	owns          []ownInput
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

func (b *ComponentReconcilerBuilder) Watches(object client.Object, opts ...builder.WatchesOption) *ComponentReconcilerBuilder {
	b.watches = append(b.watches, watchInput{
		object:       object,
		eventHandler: handlers.ToOwner(),
		options:      slices.Clone(opts),
	})

	return b
}

func (b *ComponentReconcilerBuilder) WatchesH(object client.Object, eventHandler handler.EventHandler, opts ...builder.WatchesOption) *ComponentReconcilerBuilder {
	b.watches = append(b.watches, watchInput{
		object:       object,
		eventHandler: eventHandler,
		options:      slices.Clone(opts),
	})

	return b
}

func (b *ComponentReconcilerBuilder) WatchesGVK(gvk schema.GroupVersionKind, eventHandler handler.EventHandler, opts ...builder.WatchesOption) *ComponentReconcilerBuilder {
	u := unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)

	b.watches = append(b.watches, watchInput{
		object:       &u,
		eventHandler: eventHandler,
		options:      slices.Clone(opts),
	})

	return b
}

func (b *ComponentReconcilerBuilder) WatchesM(object client.Object, fn handler.MapFunc, opts ...builder.WatchesOption) *ComponentReconcilerBuilder {
	b.watches = append(b.watches, watchInput{
		object:       object,
		eventHandler: handler.EnqueueRequestsFromMapFunc(fn),
		options:      slices.Clone(opts),
	})

	return b
}

func (b *ComponentReconcilerBuilder) Owns(object client.Object, opts ...builder.OwnsOption) *ComponentReconcilerBuilder {
	b.owns = append(b.owns, ownInput{
		object:  object,
		options: slices.Clone(opts),
	})

	return b
}

func (b *ComponentReconcilerBuilder) WithEventFilter(p predicate.Predicate) *ComponentReconcilerBuilder {
	b.predicates = append(b.predicates, p)
	return b
}

func (b *ComponentReconcilerBuilder) Build(ctx context.Context) (*ComponentReconciler, error) {
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

	r, err := NewComponentReconciler(ctx, b.mgr, name, b.input.object)
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
		watchOpts := b.watches[i].options
		if len(watchOpts) == 0 {
			watchOpts = append(watchOpts, builder.WithPredicates(predicate.And(
				generation.New(),
				component.ForLabel(labels.ComponentPartOf, b.ownerName),
			)))
		}

		c = c.Watches(b.watches[i].object, b.watches[i].eventHandler, watchOpts...)
	}

	for i := range b.owns {
		ownOpts := b.owns[i].options
		if len(ownOpts) == 0 {
			ownOpts = append(ownOpts, builder.WithPredicates(predicate.And(
				generation.New(),
			)))
		}

		c = c.Owns(b.owns[i].object, ownOpts...)
		kinds, _, err := b.mgr.GetScheme().ObjectKinds(b.owns[i].object)
		if err != nil {
			return nil, err
		}

		for i := range kinds {
			r.AddOwnedType(kinds[i])
		}
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

	return r, c.Complete(r)
}
