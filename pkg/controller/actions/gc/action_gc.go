package gc

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc/engine"
	odhTypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	odhLabels "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

type ObjectPredicateFn func(*odhTypes.ReconciliationRequest, unstructured.Unstructured) (bool, error)
type TypePredicateFn func(*odhTypes.ReconciliationRequest, schema.GroupVersionKind) (bool, error)
type ActionOpts func(*Action)

type Action struct {
	labels            map[string]string
	selector          labels.Selector
	unremovables      map[schema.GroupVersionKind]struct{}
	gc                *engine.GC
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

func WithEngine(value *engine.GC) ActionOpts {
	return func(action *Action) {
		if value == nil {
			return
		}

		action.gc = value
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

func (a *Action) run(ctx context.Context, rr *odhTypes.ReconciliationRequest) error {
	// To avoid the expensive GC, run it only when resources have
	// been generated
	if !rr.Generated {
		return nil
	}

	ns, err := a.namespaceFn(ctx, rr)
	if err != nil {
		return fmt.Errorf("unable to compute namespace: %w", err)
	}

	// TODO: use cacher to avoid computing deletable types
	//       on each run
	err = a.gc.Refresh(ctx, rr.Client, ns)
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

	deleted, err := a.gc.Run(
		ctx,
		rr.Client,
		engine.WithSelector(
			selector,
		),
		engine.WithTypeFilter(func(ctx context.Context, gvk schema.GroupVersionKind) (bool, error) {
			if a.isUnremovable(gvk) {
				return false, nil
			}

			return a.typePredicateFn(rr, gvk)
		}),
		engine.WithObjectFilter(func(ctx context.Context, obj unstructured.Unstructured) (bool, error) {
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
		}),
	)

	if err != nil {
		return fmt.Errorf("cannot run gc: %w", err)
	}

	if deleted > 0 {
		DeletedTotal.WithLabelValues(controllerName).Add(float64(deleted))
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

	if action.gc == nil {
		action.gc = engine.New()
	}

	return action.run
}
