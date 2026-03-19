package cloudmanager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/gc"
	odhTypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// currentManagedDeps returns the set of InfrastructureDependency label values present
// in rr.Resources. Each value identifies a dependency whose resources were rendered
// in the current reconcile cycle (i.e., a Managed dependency).
func currentManagedDeps(rr *odhTypes.ReconciliationRequest) map[string]struct{} {
	deps := make(map[string]struct{})
	for i := range rr.Resources {
		if dep := resources.GetLabel(&rr.Resources[i], labels.InfrastructureDependency); dep != "" {
			deps[dep] = struct{}{}
		}
	}
	return deps
}

// newGCPredicate returns the ObjectPredicateFn used by NewGCAction.
//
// Resources with the InfrastructureGCPolicy=retain label are always kept.
// All other CCM resources are deleted when their InstanceGeneration annotation
// is older than the CR's current generation. A malformed InstanceGeneration
// annotation is treated as an error.
func newGCPredicate() gc.ObjectPredicateFn {
	// cachedRR tracks the last reconciliation request seen. When the request
	// changes (new reconcile cycle), the managed-deps set is recomputed once
	// and reused for all objects evaluated in that cycle.
	var (
		cachedRR   *odhTypes.ReconciliationRequest
		cachedDeps map[string]struct{}
	)
	return func(rr *odhTypes.ReconciliationRequest, obj unstructured.Unstructured) (bool, error) {
		if rr != cachedRR {
			cachedRR = rr
			cachedDeps = currentManagedDeps(rr)
		}

		iu := resources.GetAnnotation(&obj, annotations.InstanceUID)
		ig := resources.GetAnnotation(&obj, annotations.InstanceGeneration)

		// Missing cloud manager annotations — not a cloud manager resource, keep.
		if iu == "" || ig == "" {
			return false, nil
		}

		// Retain-labeled resource — always keep, regardless of UID or generation.
		if resources.GetLabel(&obj, labels.InfrastructureGCPolicy) == labels.GCPolicyRetain {
			return false, nil
		}

		// UID mismatch — orphaned from a different CR instance, delete.
		if iu != string(rr.Instance.GetUID()) {
			return true, nil
		}

		depLabel := resources.GetLabel(&obj, labels.InfrastructureDependency)
		if depLabel != "" {
			// Resource belongs to a specific Helm dependency (chart).
			if _, managed := cachedDeps[depLabel]; !managed {
				// Unmanaged — user manages these resources, keep.
				return false, nil
			}
			// Managed dependency — fall through to generation check.
		}
		// No dep label and no retain label — apply standard generation-based GC.

		g, err := strconv.ParseInt(ig, 10, 64)
		if err != nil {
			return false, fmt.Errorf("cannot parse InstanceGeneration annotation %q on %s/%s: %w",
				ig, obj.GetNamespace(), obj.GetName(), err)
		}
		return rr.Instance.GetGeneration() != g, nil
	}
}

// operatorNamespaceFn resolves the operator namespace for GC permission checks.
// It reads the cluster config first, falling back to the OPERATOR_NAMESPACE
// environment variable when the cluster config is unavailable.
func operatorNamespaceFn(_ context.Context, _ *odhTypes.ReconciliationRequest) (string, error) {
	if ns, err := cluster.GetOperatorNamespace(); err == nil {
		return ns, nil
	}
	if ns := os.Getenv("OPERATOR_NAMESPACE"); ns != "" {
		return ns, nil
	}
	return "", errors.New("operator namespace unavailable: cluster.Init not called and OPERATOR_NAMESPACE not set")
}

// NewGCAction returns a GC action configured for cloud manager resources.
//
// resourceID must be the normalized InfrastructurePartOf label value for this controller.
// It is the same value passed to NewReconcileAction.
// NewGCAction normalizes it internally and returns an error if empty.
//
// The GC scans all resource types the operator is authorized to delete (using the
// operator namespace for permission checks), lists resources cluster-wide filtered
// by the InfrastructurePartOf label, and evaluates each with newGCPredicate.
//
// NewGCAction must be the last action in the reconciliation pipeline. GC only runs
// when rr.Generated is true (i.e., on cache miss — when something actually changed).
// In steady state with no spec changes, GC is skipped entirely.
func NewGCAction(resourceID string) (actions.Fn, error) {
	resourceID = labels.NormalizePartOfValue(resourceID)
	if resourceID == "" {
		return nil, errors.New("NewGCAction: resourceID is required")
	}
	return gc.NewAction(
		gc.InNamespaceFn(operatorNamespaceFn),
		gc.WithLabel(labels.InfrastructurePartOf, resourceID),
		gc.WithObjectPredicate(newGCPredicate()),
		gc.WithOnlyCollectOwned(false),
	), nil
}
