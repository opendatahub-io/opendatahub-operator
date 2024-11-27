package gc

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	odhTypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhLabels "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/services/gc"
)

type PredicateFn func(*odhTypes.ReconciliationRequest, unstructured.Unstructured) (bool, error)
type ActionOpts func(*Action)

type Action struct {
	labels       map[string]string
	selector     labels.Selector
	unremovables []schema.GroupVersionKind
	gc           *gc.GC
	predicateFn  PredicateFn
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
		action.unremovables = append(action.unremovables, items...)
	}
}

func WithPredicate(value PredicateFn) ActionOpts {
	return func(action *Action) {
		if value == nil {
			return
		}

		action.predicateFn = value
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

	kind, err := resources.KindForObject(rr.Client.Scheme(), rr.Instance)
	if err != nil {
		return err
	}

	controllerName := strings.ToLower(kind)

	CyclesTotal.WithLabelValues(controllerName).Inc()

	selector := a.selector
	if selector == nil {
		selector = labels.SelectorFromSet(map[string]string{
			odhLabels.PlatformPartOf: strings.ToLower(kind),
		})
	}

	deleted, err := a.gc.Run(
		ctx,
		selector,
		func(ctx context.Context, obj unstructured.Unstructured) (bool, error) {
			if slices.Contains(a.unremovables, obj.GroupVersionKind()) {
				return false, nil
			}

			return a.predicateFn(rr, obj)
		},
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
	action.predicateFn = DefaultPredicate
	action.unremovables = make([]schema.GroupVersionKind, 0)

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
