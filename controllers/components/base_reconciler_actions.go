package components

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type DeleteResourcesAction struct {
	BaseAction
	Types  []client.Object
	Labels map[string]string
}

func (r *DeleteResourcesAction) Execute(ctx context.Context, rr *ReconciliationRequest) error {
	for i := range r.Types {
		opts := make([]client.DeleteAllOfOption, 1)
		opts = append(opts, client.MatchingLabels(r.Labels))

		namespaced, err := rr.Client.IsObjectNamespaced(r.Types[i])
		if err != nil {
			return err
		}

		if namespaced {
			opts = append(opts, client.InNamespace(rr.DSCI.Spec.ApplicationsNamespace))
		}

		err = rr.Client.DeleteAllOf(ctx, r.Types[i], opts...)
		if err != nil {
			return err
		}
	}

	return nil
}
