package deployments

import (
	"context"

	fwsd "github.com/opendatahub-io/operator-actions-framework/controller/actions/status/deployments"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

type Action = fwsd.Action

type ActionOpts = fwsd.ActionOpts

var (
	WithSelectorLabel             = fwsd.WithSelectorLabel
	WithSelectorLabels            = fwsd.WithSelectorLabels
	WithPartOfLabel               = fwsd.WithPartOfLabel
	WithConditionType             = fwsd.WithConditionType
	WithNotAvailableReason        = fwsd.WithNotAvailableReason
	WithoutAutomaticPartOfDefault = fwsd.WithoutAutomaticPartOfDefault
	InNamespace                   = fwsd.InNamespace
	InNamespaceFn                 = fwsd.InNamespaceFn
)

// NewAction creates a deployment availability check action with ODH defaults
// (ApplicationNamespace for namespace resolution).
func NewAction(opts ...ActionOpts) actions.Fn {
	defaults := []ActionOpts{
		fwsd.InNamespaceFn(func(ctx context.Context, rr *types.ReconciliationRequest) (string, error) {
			return cluster.ApplicationNamespace(ctx, rr.Client)
		}),
	}
	return fwsd.NewAction(append(defaults, opts...)...)
}
