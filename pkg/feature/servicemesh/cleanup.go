package servicemesh

import (
	"context"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/gvr"
)

var log = ctrlLog.Log.WithName("features")

func RemoveExtensionProvider(f *feature.Feature) error {
	ossmAuthzProvider := fmt.Sprintf("%s-odh-auth-provider", f.Spec.AppNamespace)

	mesh := f.Spec.ControlPlane

	smcp, err := f.DynamicClient.Resource(gvr.SMCP).
		Namespace(mesh.Namespace).
		Get(context.TODO(), mesh.Name, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Since the configuration of the extension provider is a patch, it could happen that
			// the SMCP is already gone, and there will be nothing to unpatch.
			return nil
		}
		return err
	}

	extensionProviders, found, err := unstructured.NestedSlice(smcp.Object, "spec", "techPreview", "meshConfig", "extensionProviders")
	if err != nil {
		return err
	}
	if !found {
		log.Info("no extension providers found", "feature", f.Name, "control-plane", mesh.Name, "namespace", mesh.Namespace)
		return nil
	}

	for i, v := range extensionProviders {
		extensionProvider, ok := v.(map[string]interface{})
		if !ok {
			log.Info("WARN: Unexpected type for extensionProvider will not be removed")
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
