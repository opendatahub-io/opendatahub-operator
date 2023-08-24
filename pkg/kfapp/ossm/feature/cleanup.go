package feature

import (
	"context"
	"fmt"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func RemoveTokenVolumes(feature *Feature) error {
	tokenVolume := fmt.Sprintf("%s-oauth2-tokens", feature.Spec.AppNamespace)

	gvr := schema.GroupVersionResource{
		Group:    "maistra.io",
		Version:  "v2",
		Resource: "servicemeshcontrolplanes",
	}

	meshNs := feature.Spec.Mesh.Namespace
	meshName := feature.Spec.Mesh.Name

	smcp, err := feature.dynamicClient.Resource(gvr).Namespace(meshNs).Get(context.Background(), meshName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	volumes, found, err := unstructured.NestedSlice(smcp.Object, "spec", "gateways", "ingress", "volumes")
	if err != nil {
		return err
	}
	if !found {
		log.Info("no volumes found", "smcp", meshName, "istio-ns", meshNs)
		return nil
	}

	for i, v := range volumes {
		volume, ok := v.(map[string]interface{})
		if !ok {
			fmt.Println("Unexpected type for volume")
			continue
		}

		volumeMount, found, err := unstructured.NestedMap(volume, "volumeMount")
		if err != nil {
			return err
		}
		if !found {
			fmt.Println("No volumeMount found in the volume")
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

	_, err = feature.dynamicClient.Resource(gvr).Namespace(meshNs).Update(context.Background(), smcp, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	return nil
}

func RemoveOAuthClient(feature *Feature) error {
	oauthClientName := fmt.Sprintf("%s-oauth2-client", feature.Spec.AppNamespace)
	gvr := schema.GroupVersionResource{
		Group:    "oauth.openshift.io",
		Version:  "v1",
		Resource: "oauthclients",
	}

	if _, err := feature.dynamicClient.Resource(gvr).Get(context.Background(), oauthClientName, metav1.GetOptions{}); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}

		return err
	}

	if err := feature.dynamicClient.Resource(gvr).Delete(context.Background(), oauthClientName, metav1.DeleteOptions{}); err != nil {
		log.Error(err, "failed deleting OAuthClient", "name", oauthClientName)
		return err
	}

	return nil
}

func RemoveExtensionProvider(feature *Feature) error {
	ossmAuthzProvider := fmt.Sprintf("%s-odh-auth-provider", feature.Spec.AppNamespace)

	gvr := schema.GroupVersionResource{
		Group:    "maistra.io",
		Version:  "v2",
		Resource: "servicemeshcontrolplanes",
	}

	mesh := feature.Spec.Mesh

	smcp, err := feature.dynamicClient.Resource(gvr).
		Namespace(mesh.Namespace).
		Get(context.Background(), mesh.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	extensionProviders, found, err := unstructured.NestedSlice(smcp.Object, "spec", "techPreview", "meshConfig", "extensionProviders")
	if err != nil {
		return err
	}
	if !found {
		log.Info("no extension providers found", "smcp", mesh.Name, "istio-ns", mesh.Namespace)
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

	_, err = feature.dynamicClient.Resource(gvr).
		Namespace(mesh.Namespace).
		Update(context.Background(), smcp, metav1.UpdateOptions{})

	return err

}
