package trustyai

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

const pvcStorageFormat = "PVC"

func Check(ctx context.Context, reader client.Reader, _, _ string) error {
	count, err := countPVCStorageTrustyAIServices(ctx, reader)
	if err != nil {
		return err
	}
	if count == 0 {
		return nil
	}

	return &UpgradeBlockedError{
		PVCStorageTrustyAIServices: count,
	}
}

func countPVCStorageTrustyAIServices(ctx context.Context, reader client.Reader) (int, error) {
	list, err := listTrustyAIServices(ctx, reader)
	if err != nil {
		return 0, err
	}

	count := 0
	for i := range list.Items {
		format, found, err := unstructured.NestedString(list.Items[i].Object, "spec", "storage", "format")
		if err != nil {
			return 0, fmt.Errorf(
				"reading TrustyAIService %s/%s storage format: %w",
				list.Items[i].GetNamespace(),
				list.Items[i].GetName(),
				err,
			)
		}
		if found && format == pvcStorageFormat {
			count++
		}
	}

	return count, nil
}

func listTrustyAIServices(ctx context.Context, reader client.Reader) (*unstructured.UnstructuredList, error) {
	for _, kind := range []struct {
		gvk  schema.GroupVersionKind
		name string
	}{
		{gvk: gvk.TrustyAIServiceV1, name: "TrustyAIServices v1"},
		{gvk: gvk.TrustyAIServiceV1Alpha1, name: "TrustyAIServices v1alpha1"},
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
	list.SetGroupVersionKind(gvk.TrustyAIServiceV1)

	return list, nil
}
