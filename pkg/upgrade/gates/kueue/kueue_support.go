package kueue

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

func ManagementState(ctx context.Context, reader client.Reader) (string, error) {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk.Kueue)

	err := reader.Get(ctx, client.ObjectKey{Name: componentApi.KueueInstanceName}, obj)
	switch {
	case k8serr.IsNotFound(err), meta.IsNoMatchError(err):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("getting Kueue CR: %w", err)
	}

	state, found, err := unstructured.NestedString(obj.Object, "spec", "managementState")
	switch {
	case err != nil:
		return "", fmt.Errorf("reading Kueue managementState: %w", err)
	case !found:
		return "", nil
	default:
		return state, nil
	}
}
