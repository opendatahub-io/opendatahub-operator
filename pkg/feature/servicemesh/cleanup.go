package servicemesh

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
)

func RemoveExtensionProvider(controlPlane infrav1.ControlPlaneSpec, extensionName string) feature.CleanupFunc {
	return func(ctx context.Context, cli client.Client) error {
		smcp := &unstructured.Unstructured{}
		smcp.SetGroupVersionKind(gvk.ServiceMeshControlPlane)

		if err := cli.Get(ctx, client.ObjectKey{
			Namespace: controlPlane.Namespace,
			Name:      controlPlane.Name,
		}, smcp); err != nil {
			return client.IgnoreNotFound(err)
		}

		extensionProviders, found, err := unstructured.NestedSlice(smcp.Object, "spec", "techPreview", "meshConfig", "extensionProviders")
		if err != nil {
			return err
		}
		if !found {
			return nil
		}

		removed := false

		for i, v := range extensionProviders {
			extensionProvider, ok := v.(map[string]interface{})
			if !ok {
				continue
			}

			currentExtensionName, isString := extensionProvider["name"].(string)
			if !isString {
				continue
			}
			if currentExtensionName == extensionName {
				extensionProviders = append(extensionProviders[:i], extensionProviders[i+1:]...)
				err = unstructured.SetNestedSlice(smcp.Object, extensionProviders, "spec", "techPreview", "meshConfig", "extensionProviders")
				if err != nil {
					return err
				}
				removed = true
				break
			}
		}

		if removed {
			return cli.Update(ctx, smcp)
		}

		return nil
	}
}
