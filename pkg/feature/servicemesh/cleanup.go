package servicemesh

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
)

func RemoveExtensionProvider(f *feature.Feature) error {
	ossmAuthzProvider := fmt.Sprintf("%s-auth-provider", f.Spec.AppNamespace)

	mesh := f.Spec.ControlPlane
	smcp := &unstructured.Unstructured{}
	smcp.SetGroupVersionKind(cluster.ServiceMeshControlPlaneGVK)

	if err := f.Client.Get(context.TODO(), client.ObjectKey{
		Namespace: mesh.Namespace,
		Name:      mesh.Name,
	}, smcp); err != nil {
		return client.IgnoreNotFound(err)
	}

	extensionProviders, found, err := unstructured.NestedSlice(smcp.Object, "spec", "techPreview", "meshConfig", "extensionProviders")
	if err != nil {
		return err
	}
	if !found {
		f.Log.Info("no extension providers found", "feature", f.Name, "control-plane", mesh.Name, "namespace", mesh.Namespace)
		return nil
	}

	for i, v := range extensionProviders {
		extensionProvider, ok := v.(map[string]interface{})
		if !ok {
			f.Log.Info("WARN: Unexpected type for extensionProvider will not be removed")
			continue
		}

		if extensionProvider["name"] == ossmAuthzProvider {
			extensionProviders = append(extensionProviders[:i], extensionProviders[i+1:]...)
			err = unstructured.SetNestedSlice(smcp.Object, extensionProviders, "spec", "techPreview", "meshConfig", "extensionProviders")
			if err != nil {
				return err
			}
			break
		}
	}

	return f.Client.Update(context.TODO(), smcp)
}
