package cluster

import (
	"context"
	"fmt"

	apihelpers "k8s.io/apiextensions-apiserver/pkg/apihelpers"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func GetCRD(ctx context.Context, cli client.Client, name string) (apiextensionsv1.CustomResourceDefinition, error) {
	obj := apiextensionsv1.CustomResourceDefinition{}
	err := cli.Get(ctx, client.ObjectKey{Name: name}, &obj)
	if err != nil {
		return obj, err
	}

	return obj, nil
}

func HasCRD(ctx context.Context, cli client.Client, gvk schema.GroupVersionKind) (bool, error) {
	return HasCRDWithVersion(ctx, cli, gvk.GroupKind(), gvk.Version)
}

func IsAPIAvailable(cli client.Client, gvk schema.GroupVersionKind) (bool, error) {
	_, err := cli.RESTMapper().RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		if meta.IsNoMatchError(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func HasCRDWithVersion(ctx context.Context, cli client.Client, gk schema.GroupKind, version string) (bool, error) {
	m, err := cli.RESTMapper().RESTMapping(gk, version)
	if err != nil {
		if meta.IsNoMatchError(err) {
			return false, nil
		}

		return false, err
	}

	crd, err := GetCRD(ctx, cli, m.Resource.GroupResource().String())
	switch {
	case err != nil:
		return false, client.IgnoreNotFound(err)
	case apihelpers.IsCRDConditionTrue(&crd, apiextensionsv1.Terminating):
		return false, nil
	default:
		return true, nil
	}
}

func ListGVK(ctx context.Context, cli client.Client, gvk schema.GroupVersionKind, listOptions ...client.ListOption) ([]unstructured.Unstructured, error) {
	resources := unstructured.UnstructuredList{}
	resources.SetAPIVersion(gvk.GroupVersion().String())
	resources.SetKind(gvk.Kind)

	if err := cli.List(ctx, &resources, listOptions...); err != nil {
		return nil, fmt.Errorf("failed to list resources of type %s: %w", gvk, err)
	}
	return resources.Items, nil
}
