package deleteresource

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

type Action struct {
	types  []client.Object
	labels map[string]string
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
		for k, v := range values {
			action.labels[k] = v
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
			opts = append(opts, client.InNamespace(rr.DSCI.Spec.ApplicationsNamespace))
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
