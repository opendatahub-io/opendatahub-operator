package datasciencecluster

import (
	"context"
	"fmt"
	"time"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	odhtype "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

var legacyDataSciencePipelinesGVK = schema.GroupVersionKind{
	Group:   componentApi.GroupVersion.Group,
	Version: componentApi.GroupVersion.Version,
	Kind:    "DataSciencePipelines",
}

// cleanupLegacyDataSciencePipelines removes the obsolete in-tree CR before the
// AIPipelines module CR is provisioned. The old controller no longer runs, so
// any finalizers left on its singleton must also be removed.
func cleanupLegacyDataSciencePipelines(ctx context.Context, rr *odhtype.ReconciliationRequest) error {
	legacy := &unstructured.Unstructured{}
	legacy.SetGroupVersionKind(legacyDataSciencePipelinesGVK)
	legacy.SetName("default-" + componentApi.DataSciencePipelinesComponentName)

	err := rr.Client.Get(ctx, client.ObjectKeyFromObject(legacy), legacy)
	switch {
	case k8serr.IsNotFound(err), meta.IsNoMatchError(err):
		return nil
	case err != nil:
		return fmt.Errorf("get legacy DataSciencePipelines CR: %w", err)
	}

	if len(legacy.GetFinalizers()) != 0 {
		original := legacy.DeepCopy()
		legacy.SetFinalizers(nil)
		if err := rr.Client.Patch(ctx, legacy, client.MergeFrom(original)); err != nil {
			if k8serr.IsNotFound(err) || meta.IsNoMatchError(err) {
				return nil
			}
			return fmt.Errorf("remove legacy DataSciencePipelines finalizers: %w", err)
		}
	}

	if legacy.GetDeletionTimestamp().IsZero() {
		if err := rr.Client.Delete(ctx, legacy); err != nil {
			if k8serr.IsNotFound(err) || meta.IsNoMatchError(err) {
				return nil
			}
			return fmt.Errorf("delete legacy DataSciencePipelines CR: %w", err)
		}
	}

	// Kubernetes garbage collection is asynchronous. Hold new module CR
	// provisioning until the old owner has disappeared.
	return odherrors.NewStopError("waiting for legacy DataSciencePipelines CR deletion").
		WithRequeueAfter(5 * time.Second)
}
