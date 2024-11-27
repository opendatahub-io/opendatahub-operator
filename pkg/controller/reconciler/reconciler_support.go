package reconciler

import (
	"context"
	"fmt"
	"github.com/hashicorp/go-multierror"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"strings"

	"golang.org/x/exp/slices"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/component"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/generation"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

var (
	// DefaultPredicate is the default set of predicates associated to
	// resources when there is no specific predicate configured via the
	// builder.
	//
	// It would trigger a reconciliation if either the generation or
	// metadata (labels, annotations) have changed.
	DefaultPredicate = predicate.Or(
		generation.New(),
		predicate.LabelChangedPredicate{},
		predicate.AnnotationChangedPredicate{},
	)
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

type ReconcilerBuilder struct {
	mgr          ctrl.Manager
	input        forInput
	watches      []watchInput
	predicates   []predicate.Predicate
	ownerName    string
	instanceName string
	actions      []actions.Fn
	finalizers   []actions.Fn
	errors       error
}

func ReconcilerFor(mgr ctrl.Manager, ownerName string, object client.Object, opts ...builder.ForOption) *ReconcilerBuilder {
	crb := ReconcilerBuilder{
		mgr: mgr,
	}

	gvk, err := mgr.GetClient().GroupVersionKindFor(object)
	if err != nil {
		crb.errors = multierror.Append(crb.errors, fmt.Errorf("unable to determine GVK: %w", err))
	}

	crb.input = forInput{
		object:  object,
		options: slices.Clone(opts),
		gvk:     gvk,
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

func (b *ReconcilerBuilder) Watches(object client.Object, opts ...WatchOpts) *ReconcilerBuilder {
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
			DefaultPredicate,
			// use the platform.opendatahub.io/part-of label to filter
			// events not related to the owner type
			component.ForLabel(labels.PlatformPartOf, strings.ToLower(b.input.gvk.Kind)),
		))
	}
	b.watches = append(b.watches, in)
	return b
}

func (b *ReconcilerBuilder) WatchesGVK(gvk schema.GroupVersionKind, opts ...WatchOpts) *ReconcilerBuilder {
	return b.Watches(resources.GvkToUnstructured(gvk), opts...)
}

func (b *ReconcilerBuilder) Owns(object client.Object, opts ...WatchOpts) *ReconcilerBuilder {
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
		in.predicates = append(in.predicates, DefaultPredicate)
	}

	b.watches = append(b.watches, in)

	return b
}

func (b *ReconcilerBuilder) WithEventFilter(p predicate.Predicate) *ReconcilerBuilder {
	b.predicates = append(b.predicates, p)
	return b
}

func (b *ReconcilerBuilder) OwnsGVK(gvk schema.GroupVersionKind, opts ...WatchOpts) *ReconcilerBuilder {
	return b.Owns(resources.GvkToUnstructured(gvk), opts...)
}

func (b *ReconcilerBuilder) BuildComponent(_ context.Context) (*Reconciler, error) {
	if b.errors != nil {
		return nil, b.errors
	}

	name := b.instanceName
	if name == "" {
		name = strings.ToLower(b.input.gvk.Kind)
	}

	r, err := NewReconciler(b.mgr, name, b.input.object)
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
