package modelmeshserving

import (
	"context"
	"fmt"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

func Check(ctx context.Context, reader client.Reader, _, _ string) error {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk.ModelMeshServing)

	err := reader.Get(ctx, client.ObjectKey{Name: componentApi.ModelMeshServingInstanceName}, obj)
	switch {
	case k8serr.IsNotFound(err), meta.IsNoMatchError(err):
		return nil
	case err != nil:
		return fmt.Errorf("getting ModelMeshServing CR: %w", err)
	default:
		return &UpgradeBlockedError{}
	}
}
