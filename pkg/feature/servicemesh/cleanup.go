package servicemesh

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
)

func RemoveExtensionProvider(ctx context.Context, f *feature.Feature) error {
	extensionName, errExtName := FeatureData.Authorization.ExtensionProviderName.Extract(f)
	if errExtName != nil {
		return fmt.Errorf("failed to get extension name struct: %w", errExtName)
	}

	controlPlane, err := FeatureData.ControlPlane.Extract(f)
	if err != nil {
		return fmt.Errorf("failed to get control plane struct: %w", err)
	}

	smcp := &unstructured.Unstructured{}
	smcp.SetGroupVersionKind(gvk.ServiceMeshControlPlane)

	if err := f.Client.Get(ctx, client.ObjectKey{
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
		f.Log.Info("no extension providers found", "feature", f.Name, "control-plane", controlPlane.Name, "namespace", controlPlane.Namespace)
		return nil
	}

	for i, v := range extensionProviders {
		extensionProvider, ok := v.(map[string]interface{})
		if !ok {
			f.Log.Info("WARN: Unexpected type for extensionProvider, it will not be removed")
			continue
		}
		currentExtensionName, isString := extensionProvider["name"].(string)
		if !isString {
			f.Log.Info("WARN: Unexpected type for currentExtensionName, it will not be removed")
			continue
		}
		if currentExtensionName == extensionName {
			extensionProviders = append(extensionProviders[:i], extensionProviders[i+1:]...)
			err = unstructured.SetNestedSlice(smcp.Object, extensionProviders, "spec", "techPreview", "meshConfig", "extensionProviders")
			if err != nil {
				return err
			}
			break
		}
	}

	return f.Client.Update(ctx, smcp)
}
