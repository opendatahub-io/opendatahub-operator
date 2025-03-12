package gc

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	odhTypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	odhLabels "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/services/gc"
)

type ObjectPredicateFn func(*odhTypes.ReconciliationRequest, unstructured.Unstructured) (bool, error)
type TypePredicateFn func(*odhTypes.ReconciliationRequest, schema.GroupVersionKind) (bool, error)
type ActionOpts func(*Action)

type Action struct {
	labels            map[string]string
	selector          labels.Selector
	unremovables      map[schema.GroupVersionKind]struct{}
	gc                *gc.GC
	objectPredicateFn ObjectPredicateFn
	typePredicateFn   TypePredicateFn
	onlyOwned         bool
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

func WithGC(value *gc.GC) ActionOpts {
	return func(action *Action) {
		if value == nil {
			return
		}

		action.gc = value
	}
}

func (a *Action) run(ctx context.Context, rr *odhTypes.ReconciliationRequest) error {
	// To avoid the expensive GC, run it only when resources have
	// been generated
	if !rr.Generated {
		return nil
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
		selector,
		gc.WithTypeFilter(func(ctx context.Context, kind schema.GroupVersionKind) (bool, error) {
			if _, ok := a.unremovables[kind]; ok {
				return false, nil
			}

			return a.typePredicateFn(rr, kind)
		}),
		gc.WithObjectFilter(func(ctx context.Context, obj unstructured.Unstructured) (bool, error) {
			if _, ok := a.unremovables[obj.GroupVersionKind()]; ok {
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

func NewAction(opts ...ActionOpts) actions.Fn {
	action := Action{}
	action.objectPredicateFn = DefaultObjectPredicate
	action.typePredicateFn = DefaultTypePredicate
	action.onlyOwned = true
	action.unremovables = make(map[schema.GroupVersionKind]struct{})

	for _, opt := range opts {
		opt(&action)
	}

	if len(action.labels) > 0 {
		action.selector = labels.SelectorFromSet(action.labels)
	}

	// TODO: refactor
	if action.gc == nil {
		action.gc = gc.Instance
	}

	return action.run
}
