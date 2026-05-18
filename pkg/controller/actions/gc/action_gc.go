package gc

import (
	"context"

	fwgc "github.com/opendatahub-io/operator-actions-framework/controller/actions/gc"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	odhTypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

type ObjectPredicateFn = fwgc.ObjectPredicateFn

type TypePredicateFn = fwgc.TypePredicateFn

type ActionOpts = fwgc.ActionOpts

type Action = fwgc.Action

var (
	WithLabel                   = fwgc.WithLabel
	WithLabels                  = fwgc.WithLabels
	WithPartOfLabel             = fwgc.WithPartOfLabel
	WithManagedByAnnotation     = fwgc.WithManagedByAnnotation
	WithUnremovables            = fwgc.WithUnremovables
	WithObjectPredicate         = fwgc.WithObjectPredicate
	WithTypePredicate           = fwgc.WithTypePredicate
	WithOnlyCollectOwned        = fwgc.WithOnlyCollectOwned
	InNamespace                 = fwgc.InNamespace
	InNamespaceFn               = fwgc.InNamespaceFn
	WithDeletePropagationPolicy = fwgc.WithDeletePropagationPolicy
)

func defaultNamespaceFn(_ context.Context, _ *odhTypes.ReconciliationRequest) (string, error) {
	return cluster.GetOperatorNamespace()
}

// NewAction creates a new GC action with ODH defaults (operator namespace).
func NewAction(opts ...ActionOpts) actions.Fn {
	return fwgc.NewAction(defaultNamespaceFn, opts...)
}
