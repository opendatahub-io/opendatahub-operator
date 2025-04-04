package gc

import (
	"context"
	"fmt"
	"strings"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	odhTypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	odhLabels "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/rules"
)

type ObjectPredicateFn func(*odhTypes.ReconciliationRequest, unstructured.Unstructured) (bool, error)
type TypePredicateFn func(*odhTypes.ReconciliationRequest, schema.GroupVersionKind) (bool, error)
type ActionOpts func(*Action)

type Action struct {
	labels            map[string]string
	selector          labels.Selector
	propagationPolicy client.PropagationPolicy
	unremovables      map[schema.GroupVersionKind]struct{}
	objectPredicateFn ObjectPredicateFn
	typePredicateFn   TypePredicateFn
	onlyOwned         bool
	namespaceFn       actions.StringGetter
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

		for k, v := range values {
			action.labels[k] = v
		}
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

func InNamespaceFn(fn actions.StringGetter) ActionOpts {
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
	// To avoid the expensive GC, run it only when resources have
	// been generated
	if !rr.Generated {
		return nil
	}

	l := logf.FromContext(ctx)

	// TODO: use cacher to avoid computing deletable types
	//       on each run
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

	selector := a.selector
	if selector == nil {
		selector = labels.SelectorFromSet(map[string]string{
			odhLabels.PlatformPartOf: controllerName,
		})
	}

	deleted := 0
	lo := metav1.ListOptions{
		LabelSelector: selector.String(),
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

		for i := range items {
			canBeDeleted, err = a.isObjectDeletable(rr, igvk, items[i])
			if err != nil {
				return fmt.Errorf("cannot determine if object %s in namespace %q can be deleted: %w",
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
				return err
			}

			deleted++
		}
	}

	if deleted > 0 {
		DeletedTotal.WithLabelValues(controllerName).Add(float64(deleted))
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

	items, err := rules.ListAuthorizedDeletableResources(ctx, rr.Client, res, ns)
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
	if resources.HasAnnotation(&obj, annotations.ManagedByODHOperator, "false") {
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

func (a *Action) isUnremovable(gvk schema.GroupVersionKind) bool {
	_, ok := a.unremovables[gvk]
	return ok
}

func NewAction(opts ...ActionOpts) actions.Fn {
	action := Action{}
	action.objectPredicateFn = DefaultObjectPredicate
	action.typePredicateFn = DefaultTypePredicate
	action.onlyOwned = true
	action.namespaceFn = actions.OperatorNamespace
	action.propagationPolicy = client.PropagationPolicy(metav1.DeletePropagationForeground)

	// default unremovables
	action.unremovables = make(map[schema.GroupVersionKind]struct{})
	action.unremovables[gvk.CustomResourceDefinition] = struct{}{}
	action.unremovables[gvk.Lease] = struct{}{}

	for _, opt := range opts {
		opt(&action)
	}

	if len(action.labels) > 0 {
		action.selector = labels.SelectorFromSet(action.labels)
	}

	return action.run
}
