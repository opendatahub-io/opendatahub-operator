package reconciler

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/opendatahub-io/operator-actions-framework/api"
	"github.com/opendatahub-io/operator-actions-framework/cluster"
	"github.com/opendatahub-io/operator-actions-framework/controller/actions"
	"github.com/opendatahub-io/operator-actions-framework/controller/actions/dynamicownership"
	"github.com/opendatahub-io/operator-actions-framework/controller/handlers"
	"github.com/opendatahub-io/operator-actions-framework/controller/predicates"
	"github.com/opendatahub-io/operator-actions-framework/controller/predicates/label"
	"github.com/opendatahub-io/operator-actions-framework/controller/types"
	"github.com/opendatahub-io/operator-actions-framework/metadata"
	"github.com/opendatahub-io/operator-actions-framework/resources"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	DefaultPartOfLabel      = "platform.opendatahub.io/part-of"
	DefaultAnnotationPrefix = "platform.opendatahub.io"
)

var (
	namespaceGVK = schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Namespace"}
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

// CrdExists is a DynamicPredicate that checks if a given CRD identified by its GVK exists.
func CrdExists(crdGvk schema.GroupVersionKind) DynamicPredicate {
	return func(ctx context.Context, request *types.ReconciliationRequest) bool {
		if hasCrd, err := cluster.HasCRD(ctx, request.Client, crdGvk); err != nil {
			return false
		} else {
			return hasCrd
		}
	}
}

// CrdExistsWithoutPreferred returns a DynamicPredicate for whatever version is being served,
// avoiding redundant watches and API deprecation warnings.
func CrdExistsWithoutPreferred(fallbackGVK, preferredGVK schema.GroupVersionKind) DynamicPredicate {
	return func(ctx context.Context, request *types.ReconciliationRequest) bool {
		if preferred, _ := cluster.HasCRD(ctx, request.Client, preferredGVK); preferred {
			return false
		}
		fallback, _ := cluster.HasCRD(ctx, request.Client, fallbackGVK)
		return fallback
	}
}

type ReconcilerBuilder[T api.PlatformObject] struct {
	mgr                      ctrl.Manager
	input                    forInput
	watches                  []watchInput
	predicates               []predicate.Predicate
	instanceName             string
	actions                  []actions.Fn
	finalizers               []actions.Fn
	errors                   error
	happyCondition           string
	dependentConditions      []string
	dynamicOwnership         bool
	excludeFromOwnership     []schema.GroupVersionKind
	dynamicOwnershipGVKPreds map[schema.GroupVersionKind][]predicate.Predicate
	reconcilerOpts           []ReconcilerOpt
	skipStatusConditionsFn   func() bool

	instanceAnnotation string
	partOfLabel        string
}

func ReconcilerFor[T api.PlatformObject](mgr ctrl.Manager, object T, opts ...builder.ForOption) *ReconcilerBuilder[T] {
	crb := ReconcilerBuilder[T]{
		mgr:                 mgr,
		happyCondition:      DefaultHappyCondition,
		dependentConditions: []string{DefaultProvisioningConditionType},
		instanceAnnotation:  DefaultAnnotationPrefix + metadata.SuffixInstanceName,
		partOfLabel:         DefaultPartOfLabel,
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

func (b *ReconcilerBuilder[T]) WithConditions(dependents ...string) *ReconcilerBuilder[T] {
	b.dependentConditions = append(b.dependentConditions, dependents...)
	return b
}

func (b *ReconcilerBuilder[T]) WithInstanceName(instanceName string) *ReconcilerBuilder[T] {
	b.instanceName = instanceName
	return b
}

// WithInstanceAnnotation sets the annotation key used to find the owner instance name
// for non-owned Watches. Defaults to "platform.opendatahub.io/instance.name".
func (b *ReconcilerBuilder[T]) WithInstanceAnnotation(key string) *ReconcilerBuilder[T] {
	b.instanceAnnotation = key
	return b
}

// WithPartOfLabel sets the label key used for the default watch predicate
// for non-owned Watches. Defaults to "platform.opendatahub.io/part-of".
func (b *ReconcilerBuilder[T]) WithPartOfLabel(key string) *ReconcilerBuilder[T] {
	b.partOfLabel = key
	return b
}

// WithoutConditionCleanup disables automatic stale condition cleanup after reconciliation.
func (b *ReconcilerBuilder[T]) WithoutConditionCleanup() *ReconcilerBuilder[T] {
	b.reconcilerOpts = append(b.reconcilerOpts, WithSkipConditionCleanup())
	return b
}

func (b *ReconcilerBuilder[T]) WithoutStatusConditions() *ReconcilerBuilder[T] {
	b.skipStatusConditionsFn = func() bool { return true }
	return b
}

// WithoutStatusConditionsIf conditionally strips conditions from the
// status apply. When the predicate returns true, conditions are not
// applied — the controller effectively becomes a non-status-writer.
// Use this when ownership of status conditions should transfer between
// controllers at runtime (e.g. DSC owns status while in-tree components
// exist, modules controller owns it after migration).
func (b *ReconcilerBuilder[T]) WithoutStatusConditionsIf(pred func() bool) *ReconcilerBuilder[T] {
	b.skipStatusConditionsFn = pred
	return b
}

// WithReconcilerOpts passes additional functional options to the underlying Reconciler
// created during Build. Use this to set release info, finalizer name, phase names, etc.
func (b *ReconcilerBuilder[T]) WithReconcilerOpts(opts ...ReconcilerOpt) *ReconcilerBuilder[T] {
	b.reconcilerOpts = append(b.reconcilerOpts, opts...)
	return b
}

func (b *ReconcilerBuilder[T]) WithAction(value actions.Fn) *ReconcilerBuilder[T] {
	b.actions = append(b.actions, value)
	return b
}

// WithActionE is like WithAction but accepts a (Fn, error) pair from action
// constructors. If err is non-nil, the error is collected and surfaced by Build().
func (b *ReconcilerBuilder[T]) WithActionE(value actions.Fn, err error) *ReconcilerBuilder[T] {
	if err != nil {
		b.errors = multierror.Append(b.errors, err)
		return b
	}
	if value == nil {
		b.errors = multierror.Append(b.errors, errors.New("WithActionE: action must not be nil"))
		return b
	}
	b.actions = append(b.actions, value)
	return b
}

func (b *ReconcilerBuilder[T]) WithFinalizer(value actions.Fn) *ReconcilerBuilder[T] {
	b.finalizers = append(b.finalizers, value)
	return b
}

// DynamicOwnershipOption configures dynamic ownership behavior.
type DynamicOwnershipOption func(*dynamicOwnershipConfig)

type dynamicOwnershipConfig struct {
	excludeGVKs   []schema.GroupVersionKind
	gvkPredicates map[schema.GroupVersionKind][]predicate.Predicate
}

// ExcludeGVKs excludes GVKs from dynamic ownership. Excluded GVKs will not get
// owner references or watches from the dynamic ownership action. They will still
// be deployed, but the user is responsible for managing watches explicitly
// (e.g., via .Watches()/.WatchesGVK() for non-owned resources, or .Owns()/.OwnsGVK()
// for owned resources).
// Static ownership via .Owns()/.OwnsGVK() takes precedence over this exclusion.
func ExcludeGVKs(gvks ...schema.GroupVersionKind) DynamicOwnershipOption {
	return func(c *dynamicOwnershipConfig) {
		c.excludeGVKs = append(c.excludeGVKs, gvks...)
	}
}

// WithDynamicOwnershipGVKPredicates sets custom predicates for specific GVKs.
// These predicates will be used instead of the default predicates for the specified GVKs.
func WithDynamicOwnershipGVKPredicates(gvkPredicates map[schema.GroupVersionKind][]predicate.Predicate) DynamicOwnershipOption {
	return func(c *dynamicOwnershipConfig) {
		c.gvkPredicates = gvkPredicates
	}
}

// WithDynamicOwnership enables dynamic ownership mode for the reconciler.
// When enabled, the controller will automatically track ownership of resources
// that are deployed, without requiring explicit .Owns() declarations.
// This also enables watch registration for dynamically owned resources.
//
// Namespaces are always excluded from dynamic ownership: setting owner references
// on a Namespace causes cascade deletion of the entire namespace when the CR is deleted.
//
// CRDs are handled explicitly by both the deploy action (which never sets
// owner references on CRDs) and the dynamicownership action (which registers
// watches for CRDs by name rather than by owner reference).
func (b *ReconcilerBuilder[T]) WithDynamicOwnership(opts ...DynamicOwnershipOption) *ReconcilerBuilder[T] {
	b.dynamicOwnership = true

	cfg := &dynamicOwnershipConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	b.excludeFromOwnership = append(b.excludeFromOwnership, namespaceGVK)
	b.excludeFromOwnership = append(b.excludeFromOwnership, cfg.excludeGVKs...)
	b.dynamicOwnershipGVKPreds = cfg.gvkPredicates

	return b
}

func (b *ReconcilerBuilder[T]) toUnstructured(object client.Object) (*unstructured.Unstructured, error) {
	if u, ok := object.(*unstructured.Unstructured); ok {
		return u, nil
	}

	gvk, err := b.mgr.GetClient().GroupVersionKindFor(object)
	if err != nil {
		return nil, fmt.Errorf("unable to determine GVK for object: %w", err)
	}

	return resources.GvkToUnstructured(gvk), nil
}

func (b *ReconcilerBuilder[T]) Watches(object client.Object, opts ...WatchOpts) *ReconcilerBuilder[T] {
	u, err := b.toUnstructured(object)
	if err != nil {
		b.errors = multierror.Append(b.errors, fmt.Errorf("failed to convert object to unstructured for Watches: %w", err))
		return b
	}

	in := watchInput{}
	in.object = u
	in.owned = false

	for _, opt := range opts {
		opt(&in)
	}

	if in.eventHandler == nil {
		in.eventHandler = handlers.AnnotationToName(b.instanceAnnotation)
	}

	if len(in.predicates) == 0 {
		in.predicates = append(in.predicates, predicate.And(
			predicates.DefaultPredicate,
			label.ForLabel(b.partOfLabel, strings.ToLower(b.input.gvk.Kind)),
		))
	}

	b.watches = append(b.watches, in)

	return b
}

// WatchesGVK registers a watch for resources identified by GVK.
func (b *ReconcilerBuilder[T]) WatchesGVK(gvk schema.GroupVersionKind, opts ...WatchOpts) *ReconcilerBuilder[T] {
	return b.Watches(resources.GvkToUnstructured(gvk), opts...)
}

func (b *ReconcilerBuilder[T]) Owns(object client.Object, opts ...WatchOpts) *ReconcilerBuilder[T] {
	u, err := b.toUnstructured(object)
	if err != nil {
		b.errors = multierror.Append(b.errors, fmt.Errorf("failed to convert object to unstructured for Owns: %w", err))
		return b
	}

	in := watchInput{}
	in.object = u
	in.owned = true

	for _, opt := range opts {
		opt(&in)
	}

	if in.eventHandler == nil {
		in.eventHandler = handler.EnqueueRequestForOwner(
			b.mgr.GetScheme(),
			b.mgr.GetRESTMapper(),
			resources.GvkToUnstructured(b.input.gvk),
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

// ComposeWith composes the builder with the provided configuration function.
// fn receives the builder and may call any builder method on it — actions,
// watches, conditions, and finalizers registered inside fn are all applied.
// Actions land at this call position in the pipeline; watches, conditions,
// and other registrations are position-independent.
//
// fn runs immediately when ComposeWith is called, not when Build() is called.
// Passing a nil fn panics immediately.
func (b *ReconcilerBuilder[T]) ComposeWith(fn func(*ReconcilerBuilder[T])) *ReconcilerBuilder[T] {
	fn(b)
	return b
}

// OwnsGVK registers a watch for owned resources identified by GVK.
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

	opts := []ReconcilerOpt{
		WithConditionsManagerFactory(b.happyCondition, b.dependentConditions...),
	}
	if b.dynamicOwnership {
		opts = append(opts, WithDynamicOwnership(ExcludeGVKs(b.excludeFromOwnership...)))
	}

	opts = append(opts, b.reconcilerOpts...)
	if b.skipStatusConditionsFn != nil {
		opts = append(opts, withSkipStatusConditions(b.skipStatusConditionsFn))
	}

	r, err := NewReconciler(b.mgr, name, obj, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create reconciler for %s: %w", name, err)
	}

	c := ctrl.NewControllerManagedBy(b.mgr)

	forOpts := b.input.options
	if len(forOpts) == 0 {
		forOpts = append(forOpts, builder.WithPredicates(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.LabelChangedPredicate{},
			predicate.AnnotationChangedPredicate{},
		)))
	}

	c = c.For(resources.GvkToUnstructured(b.input.gvk), forOpts...)
	c = c.Named(name)

	var staticOwnedGVKs []schema.GroupVersionKind

	for i := range b.watches {
		if b.watches[i].owned {
			kinds, _, err := b.mgr.GetScheme().ObjectKinds(b.watches[i].object)
			if err != nil {
				return nil, err
			}

			for i := range kinds {
				r.AddOwnedType(kinds[i])
			}

			staticOwnedGVKs = append(staticOwnedGVKs, kinds...)
		}

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

	r.Controller = cc

	r.AddAction(
		newDynamicWatchAction(
			func(obj client.Object, eventHandler handler.EventHandler, predicates ...predicate.Predicate) error {
				return cc.Watch(source.Kind(b.mgr.GetCache(), obj, eventHandler, predicates...))
			},
			b.watches,
		),
	)

	if b.dynamicOwnership {
		dynamicOpts := []dynamicownership.Option{}
		if b.dynamicOwnershipGVKPreds != nil {
			dynamicOpts = append(dynamicOpts, dynamicownership.WithGVKPredicates(b.dynamicOwnershipGVKPreds))
		}

		if len(staticOwnedGVKs) > 0 {
			dynamicOpts = append(dynamicOpts, dynamicownership.WithPreRegistered(staticOwnedGVKs...))
		}

		r.AddAction(
			dynamicownership.NewAction(
				func(obj client.Object, eventHandler handler.EventHandler, predicates ...predicate.Predicate) error {
					return cc.Watch(source.Kind(b.mgr.GetCache(), obj, eventHandler, predicates...))
				},
				b.input.gvk,
				dynamicOpts...,
			),
		)
	}

	return r, nil
}
