package cloudmanager

import (
	"errors"
	"fmt"
	"strconv"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	certmanager "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency/certmanager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	odhTypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	odhAnnotations "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// ProtectedObject identifies a resource that GC must never delete. Matching uses
// Group+Kind (version-agnostic) plus Name and optional Namespace, so the filter
// survives API version upgrades.
type ProtectedObject struct {
	Group     string
	Kind      string
	Name      string
	Namespace string // empty for cluster-scoped resources
}

// newGCPredicate returns the ObjectPredicateFn used by NewGCAction.
//
// Unlike the generic DefaultObjectPredicate (which keeps unannotated resources),
// the CCM predicate also keeps them: resources without InstanceUID or InstanceGeneration
// annotations are not CCM-managed and should not be touched.
//
// The predicate evaluates each resource in order:
//   - Object matches a protected Group+Kind+Name+Namespace: keep.
//   - Missing InstanceUID or InstanceGeneration annotations: keep (not a CCM resource).
//   - UID mismatch with the current CR: delete (orphaned from a different CR instance).
//   - Generation mismatch with the current CR: delete (stale resource).
func newGCPredicate(protectedObjects []ProtectedObject) gc.ObjectPredicateFn {
	log := logf.Log.WithName("ccm-gc")
	protected := make(map[ProtectedObject]struct{}, len(protectedObjects))
	for _, obj := range protectedObjects {
		protected[obj] = struct{}{}
	}

	return func(rr *odhTypes.ReconciliationRequest, obj unstructured.Unstructured) (bool, error) {
		objGVK := obj.GroupVersionKind()
		key := ProtectedObject{Group: objGVK.Group, Kind: objGVK.Kind, Namespace: obj.GetNamespace(), Name: obj.GetName()}
		if _, ok := protected[key]; ok {
			log.V(3).Info("GC: keeping protected resource", "gvk", objGVK, "name", obj.GetName(), "namespace", obj.GetNamespace())
			return false, nil
		}

		iUID := resources.GetAnnotation(&obj, labels.ODHInfrastructurePrefix+odhAnnotations.SuffixInstanceUID)
		iGeneration := resources.GetAnnotation(&obj, labels.ODHInfrastructurePrefix+odhAnnotations.SuffixInstanceGeneration)

		if iUID == "" || iGeneration == "" {
			return false, nil
		}

		if iUID != string(rr.Instance.GetUID()) {
			log.V(3).Info("GC: deleting orphaned resource (UID mismatch)", "gvk", objGVK, "name", obj.GetName(), "namespace", obj.GetNamespace())
			return true, nil
		}

		iGenerationInt, err := strconv.ParseInt(iGeneration, 10, 64)
		if err != nil {
			log.Error(err, "cannot parse InstanceGeneration annotation, skipping resource",
				"annotation", iGeneration, "gvk", objGVK, "name", obj.GetName(), "namespace", obj.GetNamespace())

			return false, nil
		}

		shouldDelete := rr.Instance.GetGeneration() != iGenerationInt
		if shouldDelete {
			log.V(3).Info("GC: deleting stale resource (generation mismatch)", "gvk", objGVK, "name", obj.GetName(), "namespace", obj.GetNamespace(),
				"resourceGeneration", iGenerationInt, "crGeneration", rr.Instance.GetGeneration())
		}

		return shouldDelete, nil
	}
}

// BootstrapProtectedObjects returns the ProtectedObject entries for the PKI resources
// created by the cert-manager bootstrap action.
//
// Only the long-lived PKI infrastructure is protected: the self-signed ClusterIssuer,
// the root CA Certificate, and the CA-backed ClusterIssuer. The webhook Certificate
// is intentionally excluded because it is recreated on every reconcile with a fresh
// generation annotation, so GC will never see a stale version.
func BootstrapProtectedObjects(config certmanager.BootstrapConfig) []ProtectedObject {
	return []ProtectedObject{
		{Group: gvk.CertManagerClusterIssuer.Group, Kind: gvk.CertManagerClusterIssuer.Kind, Name: config.IssuerName},
		{Group: gvk.CertManagerCertificate.Group, Kind: gvk.CertManagerCertificate.Kind, Name: config.CertName, Namespace: config.CertManagerNamespace},
		{Group: gvk.CertManagerClusterIssuer.Group, Kind: gvk.CertManagerClusterIssuer.Kind, Name: config.CAIssuerName},
	}
}

// NewGCAction returns a GC action configured for cloud manager resources.
//
// resourceID must be the normalized InfrastructurePartOf label value for this controller.
// NewGCAction normalizes it internally and returns an error if empty.
//
// The GC scans all resource types the operator is authorized to delete (using the
// operator namespace for permission checks), lists resources cluster-wide filtered
// by the InfrastructurePartOf label, and evaluates each with newGCPredicate.
// Only owned resources are processed.
//
// NewGCAction must be the last action in the reconciliation pipeline. GC only runs
// when rr.Generated is true (i.e., on cache miss — when something actually changed).
// In steady state with no spec changes, GC is skipped entirely.
func NewGCAction(resourceID string, operatorNamespace string, protectedObjects []ProtectedObject) (actions.Fn, error) {
	resourceID = labels.NormalizePartOfValue(resourceID)
	if resourceID == "" {
		return nil, errors.New("NewGCAction: resourceID is required")
	}

	if operatorNamespace == "" {
		return nil, errors.New("NewGCAction: operatorNamespace is required")
	}

	for i, po := range protectedObjects {
		if po.Kind == "" || po.Name == "" {
			return nil, fmt.Errorf("NewGCAction: protectedObjects[%d] requires both Kind and Name", i)
		}
	}

	return gc.NewAction(
		gc.InNamespace(operatorNamespace),
		gc.WithLabel(labels.InfrastructurePartOf, resourceID),
		gc.WithObjectPredicate(newGCPredicate(protectedObjects)),
		gc.WithUnremovables(gvk.Namespace),
		gc.WithOnlyCollectOwned(true),
	), nil
}
