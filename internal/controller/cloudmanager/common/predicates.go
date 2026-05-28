package common

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
)

// OperatorCRGVKPredicates returns a WithGVKPredicates option that configures
// ResourceVersionChangedPredicate for operator CR GVKs (Istio, CertManager,
// LeaderWorkerSetOperator). This ensures status-only changes on these CRs
// trigger re-reconciliation, which DefaultPredicate (GenerationChangedPredicate)
// would otherwise filter out.
func OperatorCRGVKPredicates() reconciler.DynamicOwnershipOption {
	return reconciler.WithGVKPredicates(map[schema.GroupVersionKind][]predicate.Predicate{
		gvk.Istio:                     {predicate.ResourceVersionChangedPredicate{}},
		gvk.CertManagerV1Alpha1:       {predicate.ResourceVersionChangedPredicate{}},
		gvk.LeaderWorkerSetOperatorV1: {predicate.ResourceVersionChangedPredicate{}},
	})
}
