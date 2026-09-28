package reconciler

import (
	"context"

	fwreconciler "github.com/opendatahub-io/odh-platform-utilities/framework/controller/reconciler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/precondition"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

type Reconciler = fwreconciler.Reconciler

type ReconcilerOpt = fwreconciler.ReconcilerOpt

type PreApplyFn = fwreconciler.PreApplyFn

type PostStatusFn = fwreconciler.PostStatusFn

var (
	WithPostStatusFn              = fwreconciler.WithPostStatusFn
	WithConditionsManagerFactory  = fwreconciler.WithConditionsManagerFactory
	WithRelease                   = fwreconciler.WithRelease
	WithFinalizerName             = fwreconciler.WithFinalizerName
	WithProvisioningConditionType = fwreconciler.WithProvisioningConditionType
	WithPhaseNames                = fwreconciler.WithPhaseNames
	WithDynamicOwnership          = fwreconciler.WithDynamicOwnership
	WithPreApplyFn                = fwreconciler.WithPreApplyFn
	WithPreApplyFailedReason      = fwreconciler.WithPreApplyFailedReason
	WithSkipConditionCleanup      = fwreconciler.WithSkipConditionCleanup
)

// WithPreConditions returns a ReconcilerOpt that wires a slice of preconditions
// as the framework's PreApplyFn hook.
func WithPreConditions(pcs []precondition.PreCondition) ReconcilerOpt {
	return fwreconciler.WithPreApplyFn(func(ctx context.Context, rr *types.ReconciliationRequest) bool {
		return precondition.RunAll(ctx, rr, pcs)
	})
}

func WithDefaultPredicates(preds ...predicate.Predicate) DynamicOwnershipOption {
	return fwreconciler.WithDefaultPredicates(preds...)
}

// NewReconciler creates a new reconciler with ODH defaults
// (Release from cluster.GetRelease()).
func NewReconciler[T common.PlatformObject](mgr manager.Manager, name string, object T, opts ...ReconcilerOpt) (*Reconciler, error) {
	rel := cluster.GetRelease()
	defaults := make([]ReconcilerOpt, 0, 1+len(opts))
	defaults = append(defaults, fwreconciler.WithRelease(rel))

	return fwreconciler.NewReconciler(mgr, name, object, append(defaults, opts...)...)
}
