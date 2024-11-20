package reconciler

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

type dynamicWatchAction struct {
	mgr        ctrl.Manager
	controller controller.Controller
	watches    []watchInput
	watched    map[schema.GroupVersionKind]struct{}
}

func (a *dynamicWatchAction) run(_ context.Context, rr *types.ReconciliationRequest) error {
	controllerName := strings.ToLower(rr.Instance.GetObjectKind().GroupVersionKind().Kind)

	for i := range a.watches {
		w := a.watches[i]
		gvk := w.object.GetObjectKind().GroupVersionKind()

		if _, ok := a.watched[gvk]; ok {
			// already registered
			continue
		}

		err := a.controller.Watch(
			source.Kind(a.mgr.GetCache(), w.object),
			w.eventHandler,
			w.predicates...,
		)

		if err != nil {
			return fmt.Errorf("failed to create watcher for %s: %w", w.object.GetObjectKind().GroupVersionKind(), err)
		}

		DynamicWatchResourcesTotal.WithLabelValues(controllerName).Inc()
		a.watched[gvk] = struct{}{}
	}

	return nil
}

func newDynamicWatchAction(mgr ctrl.Manager, controller controller.Controller, watches []watchInput) actions.Fn {
	action := dynamicWatchAction{
		mgr:        mgr,
		controller: controller,
		watched:    map[schema.GroupVersionKind]struct{}{},
	}

	for i := range watches {
		if !watches[i].dynamic {
			// not dynamic
			continue
		}

		action.watches = append(action.watches, watches[i])
	}

	return action.run
}
