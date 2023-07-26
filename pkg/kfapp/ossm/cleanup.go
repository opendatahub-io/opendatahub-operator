package ossm

import (
	"context"
	"fmt"
	"github.com/hashicorp/go-multierror"
	"github.com/opendatahub-io/opendatahub-operator/apis/ossm.plugins.kubeflow.org/v1alpha1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

type cleanup func() error

func (o *OssmInstaller) CleanupOwnedResources() error {
	var cleanupErrors *multierror.Error
	for _, cleanupFunc := range o.cleanupFuncs {
		cleanupErrors = multierror.Append(cleanupErrors, cleanupFunc())
	}

	return cleanupErrors.ErrorOrNil()
}

func (o *OssmInstaller) onCleanup(cleanupFunc ...cleanup) {
	o.cleanupFuncs = append(o.cleanupFuncs, cleanupFunc...)
}

// createResourceTracker instantiates OssmResourceTracker for given KfDef application in a namespace.
// This cluster-scoped resource is used as OwnerReference in all objects OssmInstaller is created across the cluster.
// Once created, there's a cleanup function added which will be invoked on deletion of the KfDef.
func (o *OssmInstaller) createResourceTracker() error {
	tracker := &v1alpha1.OssmResourceTracker{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "ossm.plugins.kubeflow.org/v1alpha1",
			Kind:       "OssmResourceTracker",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: o.KfConfig.Name + "." + o.KfConfig.Namespace,
		},
	}

	c, err := dynamic.NewForConfig(o.config)
	if err != nil {
		return err
	}

	gvr := schema.GroupVersionResource{
		Group:    "ossm.plugins.kubeflow.org",
		Version:  "v1alpha1",
		Resource: "ossmresourcetrackers",
	}

	foundTracker, err := c.Resource(gvr).Get(context.Background(), tracker.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		unstructuredTracker, err := runtime.DefaultUnstructuredConverter.ToUnstructured(tracker)
		if err != nil {
			return err
		}

		u := unstructured.Unstructured{Object: unstructuredTracker}

		foundTracker, err = c.Resource(gvr).Create(context.Background(), &u, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	o.tracker = &v1alpha1.OssmResourceTracker{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(foundTracker.Object, o.tracker); err != nil {
		return err
	}

	o.onCleanup(func() error {
		err := c.Resource(gvr).Delete(context.Background(), o.tracker.Name, metav1.DeleteOptions{})
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return err
	})

	return nil
}

func (o *OssmInstaller) ingressVolumesRemoval() cleanup {

	return func() error {
		spec, err := o.GetPluginSpec()
		if err != nil {
			return err
		}

		tokenVolume := fmt.Sprintf("%s-oauth2-tokens", o.KfConfig.Namespace)

		dynamicClient, err := dynamic.NewForConfig(o.config)
		if err != nil {
			return err
		}

		gvr := schema.GroupVersionResource{
			Group:    "maistra.io",
			Version:  "v2",
			Resource: "servicemeshcontrolplanes",
		}

		smcp, err := dynamicClient.Resource(gvr).Namespace(spec.Mesh.Namespace).Get(context.Background(), spec.Mesh.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		volumes, found, err := unstructured.NestedSlice(smcp.Object, "spec", "gateways", "ingress", "volumes")
		if err != nil {
			return err
		}
		if !found {
			log.Info("no volumes found", "smcp", spec.Mesh.Name, "istio-ns", spec.Mesh.Namespace)
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

		_, err = dynamicClient.Resource(gvr).Namespace(spec.Mesh.Namespace).Update(context.Background(), smcp, metav1.UpdateOptions{})
		if err != nil {
			return err
		}

		return nil
	}

}

func (o *OssmInstaller) oauthClientRemoval() func() error {

	return func() error {
		c, err := dynamic.NewForConfig(o.config)
		if err != nil {
			return err
		}

		oauthClientName := fmt.Sprintf("%s-oauth2-client", o.KfConfig.Namespace)
		gvr := schema.GroupVersionResource{
			Group:    "oauth.openshift.io",
			Version:  "v1",
			Resource: "oauthclients",
		}

		if _, err := c.Resource(gvr).Get(context.Background(), oauthClientName, metav1.GetOptions{}); err != nil {
			if k8serrors.IsNotFound(err) {
				return nil
			}

			return err
		}

		if err := c.Resource(gvr).Delete(context.Background(), oauthClientName, metav1.DeleteOptions{}); err != nil {
			log.Error(err, "failed deleting OAuthClient", "name", oauthClientName)
			return err
		}

		return nil
	}
}

func (o *OssmInstaller) externalAuthzProviderRemoval() cleanup {

	return func() error {
		spec, err := o.GetPluginSpec()
		if err != nil {
			return err
		}

		ossmAuthzProvider := fmt.Sprintf("%s-odh-auth-provider", o.KfConfig.Namespace)

		dynamicClient, err := dynamic.NewForConfig(o.config)
		if err != nil {
			return err
		}

		gvr := schema.GroupVersionResource{
			Group:    "maistra.io",
			Version:  "v2",
			Resource: "servicemeshcontrolplanes",
		}

		smcp, err := dynamicClient.Resource(gvr).Namespace(spec.Mesh.Namespace).Get(context.Background(), spec.Mesh.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		extensionProviders, found, err := unstructured.NestedSlice(smcp.Object, "spec", "techPreview", "meshConfig", "extensionProviders")
		if err != nil {
			return err
		}
		if !found {
			log.Info("no extension providers found", "smcp", spec.Mesh.Name, "istio-ns", spec.Mesh.Namespace)
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

		_, err = dynamicClient.Resource(gvr).Namespace(spec.Mesh.Namespace).Update(context.Background(), smcp, metav1.UpdateOptions{})
		if err != nil {
			return err
		}

		return nil
	}
}
