package ray

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

const (
	finalizerCodeFlareOAuth = "ray.openshift.ai/oauth-finalizer"
)

func Check(ctx context.Context, reader client.Reader, _, _ string) error {
	list, err := listRayClusters(ctx, reader)
	if err != nil {
		return err
	}

	blocking := &UpgradeBlockedError{}
	for i := range list.Items {
		if !resources.HasFinalizer(&list.Items[i], finalizerCodeFlareOAuth) {
			continue
		}
		blocking.CodeFlareManagedRayClusters++
	}

	if blocking.CodeFlareManagedRayClusters == 0 {
		return nil
	}

	return blocking
}

func listRayClusters(ctx context.Context, reader client.Reader) (*unstructured.UnstructuredList, error) {
	for _, kind := range []struct {
		gvk  schema.GroupVersionKind
		name string
	}{
		{gvk: gvk.RayClusterV1, name: "RayClusters v1"},
		{gvk: gvk.RayClusterV1Alpha1, name: "RayClusters v1alpha1"},
	} {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(kind.gvk)

		err := reader.List(ctx, list)
		switch {
		case meta.IsNoMatchError(err):
			continue
		case err != nil:
			return nil, fmt.Errorf("listing %s: %w", kind.name, err)
		default:
			return list, nil
		}
	}

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk.RayClusterV1)

	return list, nil
}
