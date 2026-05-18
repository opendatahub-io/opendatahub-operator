package reconciler

import (
	"context"

	fwapi "github.com/opendatahub-io/operator-actions-framework/api"
	fwreconciler "github.com/opendatahub-io/operator-actions-framework/controller/reconciler"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/precondition"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

type Reconciler = fwreconciler.Reconciler

type ReconcilerOpt = fwreconciler.ReconcilerOpt

type PreApplyFn = fwreconciler.PreApplyFn

var (
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

// NewReconciler creates a new reconciler with ODH defaults
// (Release from cluster.GetRelease()).
func NewReconciler[T common.PlatformObject](mgr manager.Manager, name string, object T, opts ...ReconcilerOpt) (*Reconciler, error) {
	rel := cluster.GetRelease()
	defaults := []ReconcilerOpt{
		fwreconciler.WithRelease(fwapi.Release{Name: rel.Name, Version: rel.Version.Version}),
	}

	return fwreconciler.NewReconciler(mgr, name, object, append(defaults, opts...)...)
}
