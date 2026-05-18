package reconciler

import (
	"context"

	fwapi "github.com/opendatahub-io/operator-actions-framework/api"
	fwreconciler "github.com/opendatahub-io/operator-actions-framework/controller/reconciler"
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
			fwreconciler.WithRelease(fwapi.Release{Name: rel.Name, Version: rel.Version.Version}),
		)
}
