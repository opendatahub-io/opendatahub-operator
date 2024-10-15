package actions

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const (
	DeleteResourcesActionName = "delete-resources"
)

type DeleteResourcesAction struct {
	BaseAction
	types  []client.Object
	labels map[string]string
}

type DeleteResourcesActionOpts func(*DeleteResourcesAction)

func WithDeleteResourcesTypes(values ...client.Object) DeleteResourcesActionOpts {
	return func(action *DeleteResourcesAction) {
		action.types = append(action.types, values...)
	}
}

func WithDeleteResourcesLabel(k string, v string) DeleteResourcesActionOpts {
	return func(action *DeleteResourcesAction) {
		action.labels[k] = v
	}
}

func WithDeleteResourcesLabels(values map[string]string) DeleteResourcesActionOpts {
	return func(action *DeleteResourcesAction) {
		for k, v := range values {
			action.labels[k] = v
		}
	}
}

func (r *DeleteResourcesAction) Execute(ctx context.Context, rr *types.ReconciliationRequest) error {
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

func NewDeleteResourcesAction(ctx context.Context, opts ...DeleteResourcesActionOpts) *DeleteResourcesAction {
	action := DeleteResourcesAction{
		BaseAction: BaseAction{
			Log: log.FromContext(ctx).WithName(ActionGroup).WithName(DeleteResourcesActionName),
		},
		types:  make([]client.Object, 0),
		labels: map[string]string{},
	}

	for _, opt := range opts {
		opt(&action)
	}

	return &action
}
