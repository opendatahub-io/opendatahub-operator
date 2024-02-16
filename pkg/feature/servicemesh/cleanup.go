package servicemesh

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/gvr"
)

func RemoveExtensionProvider(f *feature.Feature) error {
	ossmAuthzProvider := fmt.Sprintf("%s-odh-auth-provider", f.Spec.AppNamespace)

	mesh := f.Spec.ControlPlane

	smcp, err := f.DynamicClient.Resource(gvr.SMCP).
		Namespace(mesh.Namespace).
		Get(context.TODO(), mesh.Name, metav1.GetOptions{})
	if err != nil {
		// Since the configuration of the extension provider is a patch, it could happen that
		// the SMCP is already gone, and there will be nothing to unpatch.
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

	_, err = f.DynamicClient.Resource(gvr.SMCP).
		Namespace(mesh.Namespace).
		Update(context.TODO(), smcp, metav1.UpdateOptions{})

	return err
}
