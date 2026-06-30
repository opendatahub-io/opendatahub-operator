package gc

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/opendatahub-io/operator-actions-framework/controller/actions"
	odhTypes "github.com/opendatahub-io/operator-actions-framework/controller/types"
	"github.com/opendatahub-io/operator-actions-framework/resources"
	"github.com/opendatahub-io/operator-actions-framework/rules"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	DefaultPartOfLabelKey      = "platform.opendatahub.io/part-of"
	DefaultAnnotationPrefix    = "platform.opendatahub.io"
	DefaultManagedByAnnotation = "opendatahub.io/managed"
)

type ObjectPredicateFn func(*odhTypes.ReconciliationRequest, unstructured.Unstructured) (bool, error)
type TypePredicateFn func(*odhTypes.ReconciliationRequest, schema.GroupVersionKind) (bool, error)
type ActionOpts func(*Action)

type Action struct {
	partOfLabelKey      string
	managedByAnnotation string
	labels              map[string]string
	selector            labels.Selector
	propagationPolicy   client.PropagationPolicy
	unremovables        map[schema.GroupVersionKind]struct{}
	objectPredicateFn   ObjectPredicateFn
	typePredicateFn     TypePredicateFn
	onlyOwned           bool
	namespaceFn         actions.Getter[string]
}

func WithLabel(name string, value string) ActionOpts {
	return func(action *Action) {
		if action.labels == nil {
			action.labels = map[string]string{}
		}

		action.labels[name] = value
	}
}

func WithLabels(values map[string]string) ActionOpts {
	return func(action *Action) {
		if action.labels == nil {
			action.labels = map[string]string{}
		}

		maps.Copy(action.labels, values)
	}
}

// WithPartOfLabel overrides the label key used by getOrComputeSelector to find
// resources owned by this controller.
func WithPartOfLabel(key string) ActionOpts {
	return func(action *Action) {
		action.partOfLabelKey = key
	}
}

// WithManagedByAnnotation sets the annotation key used to determine if an object
// is explicitly marked as not managed.
func WithManagedByAnnotation(key string) ActionOpts {
	return func(action *Action) {
		action.managedByAnnotation = key
	}
}

func WithUnremovables(items ...schema.GroupVersionKind) ActionOpts {
	return func(action *Action) {
		for _, item := range items {
			action.unremovables[item] = struct{}{}
		}
	}
}

func WithObjectPredicate(value ObjectPredicateFn) ActionOpts {
	return func(action *Action) {
		if value == nil {
			return
		}

		action.objectPredicateFn = value
	}
}

func WithTypePredicate(value TypePredicateFn) ActionOpts {
	return func(action *Action) {
		if value == nil {
			return
		}

		action.typePredicateFn = value
	}
}

func WithOnlyCollectOwned(value bool) ActionOpts {
	return func(action *Action) {
		action.onlyOwned = value
	}
}

func InNamespace(ns string) ActionOpts {
	return func(action *Action) {
		action.namespaceFn = func(_ context.Context, _ *odhTypes.ReconciliationRequest) (string, error) {
			return ns, nil
		}
	}
}

func InNamespaceFn(fn actions.Getter[string]) ActionOpts {
	return func(action *Action) {
		if fn == nil {
			return
		}
		action.namespaceFn = fn
	}
}
func WithDeletePropagationPolicy(policy metav1.DeletionPropagation) ActionOpts {
	return func(action *Action) {
		action.propagationPolicy = client.PropagationPolicy(policy)
	}
}

func (a *Action) run(ctx context.Context, rr *odhTypes.ReconciliationRequest) error {
	if rr.SkipDeploy {
		return nil
	}

	if !rr.Generated {
		return nil
	}

	l := logf.FromContext(ctx)

	items, err := a.computeDeletableTypes(ctx, rr)
	if err != nil {
		return fmt.Errorf("unable to refresh collectable resources: %w", err)
	}

	igvk, err := resources.GetGroupVersionKindForObject(rr.Client.Scheme(), rr.Instance)
	if err != nil {
		return err
	}

	controllerName := strings.ToLower(igvk.Kind)

	CyclesTotal.WithLabelValues(controllerName).Inc()

	lo := metav1.ListOptions{
		LabelSelector: a.getOrComputeSelector(controllerName).String(),
	}

	l.V(3).Info("run", "selector", lo.LabelSelector)

	for _, res := range items {
		canBeDeleted, err := a.isTypeDeletable(rr, res.GroupVersionKind())
		if err != nil {
			return fmt.Errorf("cannot determine if resource %s can be deleted: %w", res.String(), err)
		}

		if !canBeDeleted {
			continue
		}

		items, err := a.listResources(ctx, rr.Controller.GetDynamicClient(), res, lo)
		if err != nil {
			return fmt.Errorf("cannot list child resources %s: %w", res.String(), err)
		}

		deleted, err := a.deleteResources(ctx, rr, igvk, items)
		if err != nil {
			return fmt.Errorf("error processing items to delete: %w", err)
		}

		if deleted > 0 {
			DeletedTotal.WithLabelValues(controllerName).Add(float64(deleted))
		}
	}

	return nil
}

func (a *Action) computeDeletableTypes(ctx context.Context, rr *odhTypes.ReconciliationRequest) ([]resources.Resource, error) {
	res, err := resources.ListAvailableAPIResources(rr.Controller.GetDiscoveryClient())
	if err != nil {
		return nil, fmt.Errorf("failure discovering resources: %w", err)
	}

	ns, err := a.namespaceFn(ctx, rr)
	if err != nil {
		return nil, fmt.Errorf("unable to compute namespace: %w", err)
	}

	items, err := rules.ListAuthorizedResources(ctx, rr.Client, res, ns, []string{rules.VerbDelete})
	if err != nil {
		return nil, fmt.Errorf("failure listing authorized deletable resources: %w", err)
	}

	return items, nil
}

func (a *Action) listResources(
	ctx context.Context,
	dc dynamic.Interface,
	res resources.Resource,
	opts metav1.ListOptions,
) ([]unstructured.Unstructured, error) {
	items, err := dc.Resource(res.GroupVersionResource()).Namespace("").List(ctx, opts)
	switch {
	case k8serr.IsForbidden(err) || k8serr.IsMethodNotSupported(err) || k8serr.IsNotFound(err):
		logf.FromContext(ctx).V(3).Info(
			"cannot list resource",
			"reason", err.Error(),
			"gvk", res.GroupVersionKind(),
		)

		return nil, nil
	case err != nil:
		return nil, err
	default:
		return items.Items, nil
	}
}

func (a *Action) isTypeDeletable(
	rr *odhTypes.ReconciliationRequest,
	gvk schema.GroupVersionKind,
) (bool, error) {
	if a.isUnremovable(gvk) {
		return false, nil
	}

	return a.typePredicateFn(rr, gvk)
}

func (a *Action) isObjectDeletable(
	rr *odhTypes.ReconciliationRequest,
	igvk schema.GroupVersionKind,
	obj unstructured.Unstructured,
) (bool, error) {
	if a.isUnremovable(obj.GroupVersionKind()) {
		return false, nil
	}
	if resources.HasAnnotation(&obj, a.managedByAnnotation, "false") {
		return false, nil
	}

	if a.onlyOwned {
		o, err := resources.IsOwnedByType(&obj, igvk)
		if err != nil {
			return false, err
		}
		if !o {
			return false, nil
		}
	}

	return a.objectPredicateFn(rr, obj)
}

func (a *Action) deleteResources(
	ctx context.Context,
	rr *odhTypes.ReconciliationRequest,
	igvk schema.GroupVersionKind,
	items []unstructured.Unstructured,
) (int, error) {
	deleted := 0

	for i := range items {
		canBeDeleted, err := a.isObjectDeletable(rr, igvk, items[i])
		if err != nil {
			return 0, fmt.Errorf("cannot determine if object %s in namespace %q can be deleted: %w",
				items[i].GetName(),
				items[i].GetNamespace(),
				err,
			)
		}

		if !canBeDeleted {
			continue
		}

		if !items[i].GetDeletionTimestamp().IsZero() {
			continue
		}

		if err := a.delete(ctx, rr.Client, items[i]); err != nil {
			return 0, err
		}

		deleted++
	}

	return deleted, nil
}

func (a *Action) delete(
	ctx context.Context,
	cli client.Client,
	resource unstructured.Unstructured,
) error {
	logf.FromContext(ctx).Info(
		"delete",
		"gvk", resource.GroupVersionKind(),
		"ns", resource.GetNamespace(),
		"name", resource.GetName(),
	)

	err := cli.Delete(ctx, &resource, a.propagationPolicy)
	if err != nil && !k8serr.IsNotFound(err) {
		return fmt.Errorf(
			"cannot delete resources gvk: %s, namespace: %s, name: %s, reason: %w",
			resource.GroupVersionKind().String(),
			resource.GetNamespace(),
			resource.GetName(),
			err,
		)
	}

	return nil
}

func (a *Action) getOrComputeSelector(partOf string) labels.Selector {
	if a.selector != nil {
		return a.selector
	}

	return labels.SelectorFromSet(map[string]string{
		a.partOfLabelKey: partOf,
	})
}

func (a *Action) isUnremovable(gvk schema.GroupVersionKind) bool {
	_, ok := a.unremovables[gvk]
	return ok
}

// NewAction creates a new GC action. The namespaceFn is required to determine
// the namespace used for RBAC rules review.
func NewAction(namespaceFn actions.Getter[string], opts ...ActionOpts) actions.Fn {
	crdGVK := schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"}
	leaseGVK := schema.GroupVersionKind{Group: "coordination.k8s.io", Version: "v1", Kind: "Lease"}

	action := Action{
		partOfLabelKey:      DefaultPartOfLabelKey,
		managedByAnnotation: DefaultManagedByAnnotation,
	}
	action.objectPredicateFn = DefaultObjectPredicate(DefaultAnnotationPrefix)
	action.typePredicateFn = DefaultTypePredicate
	action.onlyOwned = true
	action.namespaceFn = namespaceFn
	action.propagationPolicy = client.PropagationPolicy(metav1.DeletePropagationForeground)

	action.unremovables = make(map[schema.GroupVersionKind]struct{})
	action.unremovables[crdGVK] = struct{}{}
	action.unremovables[leaseGVK] = struct{}{}

	for _, opt := range opts {
		opt(&action)
	}

	if len(action.labels) > 0 {
		action.selector = labels.SelectorFromSet(action.labels)
	}

	return action.run
}
