package gc

import (
	"context"

	fwgc "github.com/opendatahub-io/odh-platform-utilities/framework/controller/actions/gc"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	odhTypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// DefaultUnremovables lists GVKs that GC never deletes by default.
// Consumers outside the gc package (e.g. cleanup actions) can reference
// this slice to apply the same rules.
var DefaultUnremovables = []schema.GroupVersionKind{
	gvk.CustomResourceDefinition,
	gvk.Lease,
}

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
