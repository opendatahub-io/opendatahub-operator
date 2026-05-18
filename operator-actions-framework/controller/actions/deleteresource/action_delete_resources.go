package deleteresource

import (
	"context"
	"maps"

	"github.com/opendatahub-io/operator-actions-framework/controller/actions"
	"github.com/opendatahub-io/operator-actions-framework/controller/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Action struct {
	types       []client.Object
	labels      map[string]string
	namespaceFn actions.Getter[string]
}

type ActionOpts func(*Action)

func WithDeleteResourcesTypes(values ...client.Object) ActionOpts {
	return func(action *Action) {
		action.types = append(action.types, values...)
	}
}

func WithDeleteResourcesLabel(k string, v string) ActionOpts {
	return func(action *Action) {
		action.labels[k] = v
	}
}

func WithDeleteResourcesLabels(values map[string]string) ActionOpts {
	return func(action *Action) {
		maps.Copy(action.labels, values)
	}
}

// WithNamespaceFn sets the function used to determine the namespace for
// namespaced resources during deletion.
func WithNamespaceFn(fn actions.Getter[string]) ActionOpts {
	return func(action *Action) {
		if fn != nil {
			action.namespaceFn = fn
		}
	}
}

func (r *Action) run(ctx context.Context, rr *types.ReconciliationRequest) error {
	for i := range r.types {
		opts := make([]client.DeleteAllOfOption, 0)

		if len(r.labels) > 0 {
			opts = append(opts, client.MatchingLabels(r.labels))
		}

		namespaced, err := rr.Client.IsObjectNamespaced(r.types[i])
		if err != nil {
			return err
		}

		if namespaced {
			if r.namespaceFn == nil {
				continue
			}
			appNamespace, nsErr := r.namespaceFn(ctx, rr)
			if nsErr != nil {
				return nsErr
			}
			opts = append(opts, client.InNamespace(appNamespace))
		}

		err = rr.Client.DeleteAllOf(ctx, r.types[i], opts...)
		if err != nil {
			return err
		}
	}

	return nil
}

func NewAction(opts ...ActionOpts) actions.Fn {
	action := Action{
		types:  make([]client.Object, 0),
		labels: map[string]string{},
	}

	for _, opt := range opts {
		opt(&action)
	}

	return action.run
}
