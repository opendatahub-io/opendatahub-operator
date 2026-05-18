package cloudmanager

import (
	"context"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/cleanup"
	certmanager "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency/certmanager"
	odhTypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// DefaultCleanupTargets returns the cleanup.Target entries for dependency CRs
// whose finalizers must be processed before GC removes their operators during
// Unmanaged transitions and during parent CR deletion.
func DefaultCleanupTargets() []cleanup.Target {
	return []cleanup.Target{
		certmanager.BootstrapCleanupTarget(),
		{GVK: gvk.Istio, Name: "default", FinalizerPrefix: "sailoperator.io/"},
	}
}

// cleanupStaleCR checks whether the target dependency CR should be cleaned up
// using isStaleOrOrphaned (the same annotation check used by the GC predicate).
// If stale, it delegates to cleanup.DeleteAndWait with the already-fetched
// object to avoid a redundant API call. Ownership is verified inside
// DeleteAndWait via OwnerReferences.
func cleanupStaleCR(ctx context.Context, rr *odhTypes.ReconciliationRequest, target cleanup.Target) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(target.GVK)

	if err := rr.Client.Get(ctx, client.ObjectKey{Name: target.Name, Namespace: target.Namespace}, obj); err != nil {
		if k8serr.IsNotFound(err) || meta.IsNoMatchError(err) {
			return nil
		}

		return err
	}

	shouldClean, err := isStaleOrOrphaned(rr, *obj)
	if err != nil || !shouldClean {
		return err
	}

	return cleanup.DeleteAndWait(ctx, rr, target, obj)
}
