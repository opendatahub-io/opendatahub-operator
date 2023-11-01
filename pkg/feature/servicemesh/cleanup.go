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

func RemoveTokenVolumes(f *feature.Feature) error {
	tokenVolume := fmt.Sprintf("%s-oauth2-tokens", f.Spec.AppNamespace)

	meshNs := f.Spec.Mesh.Namespace
	meshName := f.Spec.Mesh.Name

	smcp, err := f.DynamicClient.Resource(gvr.SMCP).Namespace(meshNs).Get(context.TODO(), meshName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	volumes, found, err := unstructured.NestedSlice(smcp.Object, "spec", "gateways", "ingress", "volumes")
	if err != nil {
		return err
	}
	if !found {
		log.Info("no volumes found", "f", f.Name, "control-plane", meshName, "istio-ns", meshNs)
		return nil
	}

	for i, v := range volumes {
		volume, ok := v.(map[string]interface{})
		if !ok {
			log.Info("unexpected type for volume", "f", f.Name, "type", fmt.Sprintf("%T", volume))
			continue
		}

		volumeMount, found, err := unstructured.NestedMap(volume, "volumeMount")
		if err != nil {
			return err
		}
		if !found {
			log.Info("no volumeMount found in the volume", "f", f.Name)
			continue
		}

		if volumeMount["name"] == tokenVolume {
			volumes = append(volumes[:i], volumes[i+1:]...)
			err = unstructured.SetNestedSlice(smcp.Object, volumes, "spec", "gateways", "ingress", "volumes")
			if err != nil {
				return err
			}
			break
		}
	}

	_, err = f.DynamicClient.Resource(gvr.SMCP).Namespace(meshNs).Update(context.TODO(), smcp, metav1.UpdateOptions{})

	return err
}

func RemoveOAuthClient(f *feature.Feature) error {
	oauthClientName := fmt.Sprintf("%s-oauth2-client", f.Spec.AppNamespace)

	if _, err := f.DynamicClient.Resource(gvr.OAuthClient).Get(context.TODO(), oauthClientName, metav1.GetOptions{}); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}

		return err
	}

	if err := f.DynamicClient.Resource(gvr.OAuthClient).Delete(context.TODO(), oauthClientName, metav1.DeleteOptions{}); err != nil {
		log.Error(err, "failed deleting OAuthClient", "f", f.Name, "name", oauthClientName)

		return err
	}

	return nil
}

func RemoveExtensionProvider(f *feature.Feature) error {
	ossmAuthzProvider := fmt.Sprintf("%s-odh-auth-provider", f.Spec.AppNamespace)

	mesh := f.Spec.Mesh

	smcp, err := f.DynamicClient.Resource(gvr.SMCP).
		Namespace(mesh.Namespace).
		Get(context.TODO(), mesh.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	extensionProviders, found, err := unstructured.NestedSlice(smcp.Object, "spec", "techPreview", "meshConfig", "extensionProviders")
	if err != nil {
		return err
	}
	if !found {
		log.Info("no extension providers found", "f", f.Name, "control-plane", mesh.Name, "namespace", mesh.Namespace)
		return nil
	}

	for i, v := range extensionProviders {
		extensionProvider, ok := v.(map[string]interface{})
		if !ok {
			fmt.Println("Unexpected type for extensionProvider")
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
