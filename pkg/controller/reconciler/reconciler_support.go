package reconciler

import (
	"context"
	"fmt"
	"github.com/opendatahub-io/opendatahub-operator/v2/apis/services"
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
	object  client.Object
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

type ReconcilerBuilder struct {
	mgr          ctrl.Manager
	input        forInput
	watches      []watchInput
	owns         []ownInput
	predicates   []predicate.Predicate
	ownerName    string
	instanceName string
	actions      []actions.Fn
	finalizers   []actions.Fn
}

func ReconcilerFor(mgr ctrl.Manager, ownerName string, object components.ComponentObject, opts ...builder.ForOption) *ReconcilerBuilder {
	crb := ReconcilerBuilder{
		mgr:       mgr,
		ownerName: ownerName,
		input: forInput{
			object:  object,
			options: slices.Clone(opts),
		},
	}

	return &crb
}

func (b *ReconcilerBuilder) WithInstanceName(instanceName string) *ReconcilerBuilder {
	b.instanceName = instanceName
	return b
}

func (b *ReconcilerBuilder) WithAction(value actions.Fn) *ReconcilerBuilder {
	b.actions = append(b.actions, value)
	return b
}

func (b *ReconcilerBuilder) WithFinalizer(value actions.Fn) *ReconcilerBuilder {
	b.finalizers = append(b.finalizers, value)
	return b
}

func (b *ReconcilerBuilder) Watches(object client.Object, opts ...builder.WatchesOption) *ReconcilerBuilder {
	var instanceLabel string
	if _, ok := b.input.object.(components.ComponentObject); ok {
		instanceLabel = labels.ComponentPartOf
	} else if _, ok := b.input.object.(components.ComponentObject); ok {
		instanceLabel = labels.ServicePartOf
	}
	b.watches = append(b.watches, watchInput{
		object:       object,
		eventHandler: handlers.ToOwner(instanceLabel),
		options:      slices.Clone(opts),
	})

	return b
}

func (b *ReconcilerBuilder) WatchesH(object client.Object, eventHandler handler.EventHandler, opts ...builder.WatchesOption) *ReconcilerBuilder {
	b.watches = append(b.watches, watchInput{
		object:       object,
		eventHandler: eventHandler,
		options:      slices.Clone(opts),
	})

	return b
}

func (b *ReconcilerBuilder) WatchesGVK(gvk schema.GroupVersionKind, eventHandler handler.EventHandler, opts ...builder.WatchesOption) *ReconcilerBuilder {
	u := unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)

	b.watches = append(b.watches, watchInput{
		object:       &u,
		eventHandler: eventHandler,
		options:      slices.Clone(opts),
	})

	return b
}

func (b *ReconcilerBuilder) WatchesM(object client.Object, fn handler.MapFunc, opts ...builder.WatchesOption) *ReconcilerBuilder {
	b.watches = append(b.watches, watchInput{
		object:       object,
		eventHandler: handler.EnqueueRequestsFromMapFunc(fn),
		options:      slices.Clone(opts),
	})

	return b
}

func (b *ReconcilerBuilder) Owns(object client.Object, opts ...builder.OwnsOption) *ReconcilerBuilder {
	b.owns = append(b.owns, ownInput{
		object:  object,
		options: slices.Clone(opts),
	})

	return b
}

func (b *ReconcilerBuilder) WithEventFilter(p predicate.Predicate) *ReconcilerBuilder {
	b.predicates = append(b.predicates, p)
	return b
}

func (b *ReconcilerBuilder) BuildComponent(ctx context.Context) (*ComponentReconciler, error) {
	name := b.instanceName
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

	obj, ok := b.input.object.(components.ComponentObject)
	if !ok {
		return nil, fmt.Errorf("invalid type for object")
	}

	r, err := NewComponentReconciler(ctx, b.mgr, name, obj)
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

func (b *ReconcilerBuilder) BuildService(ctx context.Context) (*ServiceReconciler, error) {
	name := b.instanceName
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

	obj, ok := b.input.object.(services.ServiceObject)
	if !ok {
		return nil, fmt.Errorf("invalid type for object")
	}
	r, err := NewServiceReconciler(ctx, b.mgr, name, obj)
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
				component.ForLabel(labels.ServicePartOf, b.ownerName),
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
