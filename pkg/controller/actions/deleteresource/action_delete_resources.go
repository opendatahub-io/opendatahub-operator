package deleteresource

import (
	"context"

	fwdr "github.com/opendatahub-io/operator-actions-framework/controller/actions/deleteresource"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

type Action = fwdr.Action

type ActionOpts = fwdr.ActionOpts

var (
	WithDeleteResourcesTypes  = fwdr.WithDeleteResourcesTypes
	WithDeleteResourcesLabel  = fwdr.WithDeleteResourcesLabel
	WithDeleteResourcesLabels = fwdr.WithDeleteResourcesLabels
	WithNamespaceFn           = fwdr.WithNamespaceFn
)

// NewAction creates a delete resource action with ODH defaults
// (ApplicationNamespace for namespaced resources).
func NewAction(opts ...ActionOpts) actions.Fn {
	defaults := []ActionOpts{
		fwdr.WithNamespaceFn(func(ctx context.Context, rr *types.ReconciliationRequest) (string, error) {
			return cluster.ApplicationNamespace(ctx, rr.Client)
		}),
	}
	return fwdr.NewAction(append(defaults, opts...)...)
}
