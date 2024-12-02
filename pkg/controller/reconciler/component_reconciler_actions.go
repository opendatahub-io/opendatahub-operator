package reconciler

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

type dynamicWatchFn func(client.Object, handler.EventHandler, ...predicate.Predicate) error

type dynamicWatchAction struct {
	fn      dynamicWatchFn
	watches []watchInput
	watched map[schema.GroupVersionKind]struct{}
}

func (a *dynamicWatchAction) run(ctx context.Context, rr *types.ReconciliationRequest) error {
	controllerName := strings.ToLower(rr.Instance.GetObjectKind().GroupVersionKind().Kind)

	for i := range a.watches {
		w := a.watches[i]
		gvk := w.object.GetObjectKind().GroupVersionKind()

		if _, ok := a.watched[gvk]; ok {
			// already registered
			continue
		}

		ok := a.shouldWatch(ctx, w, rr)
		if !ok {
			continue
		}

		err := a.fn(w.object, w.eventHandler, w.predicates...)
		if err != nil {
			return fmt.Errorf("failed to create watcher for %s: %w", w.object.GetObjectKind().GroupVersionKind(), err)
		}

		a.watched[gvk] = struct{}{}
		DynamicWatchResourcesTotal.WithLabelValues(controllerName).Inc()
	}

	return nil
}

func (a *dynamicWatchAction) shouldWatch(ctx context.Context, in watchInput, rr *types.ReconciliationRequest) bool {
	for pi := range in.dynamicPred {
		ok := in.dynamicPred[pi](ctx, rr)
		if !ok {
			return false
		}
	}

	return true
}

func newDynamicWatch(fn dynamicWatchFn, watches []watchInput) *dynamicWatchAction {
	action := dynamicWatchAction{
		fn:      fn,
		watched: map[schema.GroupVersionKind]struct{}{},
	}

	for i := range watches {
		if !watches[i].dynamic {
			// not dynamic
			continue
		}

		action.watches = append(action.watches, watches[i])
	}

	return &action
}

func newDynamicWatchAction(fn dynamicWatchFn, watches []watchInput) actions.Fn {
	action := newDynamicWatch(fn, watches)
	return action.run
}
