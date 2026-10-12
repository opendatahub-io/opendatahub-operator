package reconciler

import (
	"context"

	fwconditions "github.com/opendatahub-io/odh-platform-utilities/framework/controller/conditions"
	fwreconciler "github.com/opendatahub-io/odh-platform-utilities/framework/controller/reconciler"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

type DynamicPredicate = fwreconciler.DynamicPredicate

type WatchOpts = fwreconciler.WatchOpts

type ReconcilerBuilder[T common.PlatformObject] = fwreconciler.ReconcilerBuilder[T]

type DynamicOwnershipOption = fwreconciler.DynamicOwnershipOption

// DependentConditions converts operator condition names to the framework's
// condition dependency definitions. Unspecified polarity defaults to healthy when True.
func DependentConditions[T ~string](conditionTypes ...T) []fwconditions.DependentDefinition {
	return fwreconciler.DependentConditions(conditionTypes...)
}

var (
	WithPredicates    = fwreconciler.WithPredicates
	WithEventHandler  = fwreconciler.WithEventHandler
	WithEventMapper   = fwreconciler.WithEventMapper
	Dynamic           = fwreconciler.Dynamic
	ExcludeGVKs       = fwreconciler.ExcludeGVKs
	WithGVKPredicates = fwreconciler.WithDynamicOwnershipGVKPredicates

	CrdExists                 = fwreconciler.CrdExists
	CrdExistsWithoutPreferred = fwreconciler.CrdExistsWithoutPreferred
)

// ClusterIsOpenShift is a DynamicPredicate that returns true when the operator
// is running on an OpenShift cluster.
func ClusterIsOpenShift() DynamicPredicate {
	return func(_ context.Context, _ *types.ReconciliationRequest) bool {
		return cluster.GetClusterInfo().Type == cluster.ClusterTypeOpenShift
	}
}

// ReconcilerFor creates a new reconciler builder with ODH defaults
// (Release from cluster.GetRelease()).
func ReconcilerFor[T common.PlatformObject](mgr ctrl.Manager, object T, opts ...builder.ForOption) *ReconcilerBuilder[T] {
	rel := cluster.GetRelease()
	return fwreconciler.ReconcilerFor(mgr, object, opts...).
		WithReconcilerOpts(
			fwreconciler.WithRelease(rel),
		)
}
