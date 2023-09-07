package feature

import (
	"context"
	"fmt"
	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"strings"
)

func SelfSignedCertificate(feature *Feature) error {
	if feature.Spec.Mesh.Certificate.Generate {
		meta := metav1.ObjectMeta{
			Name:      feature.Spec.Mesh.Certificate.Name,
			Namespace: feature.Spec.Mesh.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				feature.OwnerReference(),
			},
		}

		cert, err := generateSelfSignedCertificateAsSecret(feature.Spec.Domain, meta)
		if err != nil {
			return errors.WithStack(err)
		}

		if err != nil {
			return errors.WithStack(err)
		}

		_, err = feature.clientset.CoreV1().
			Secrets(feature.Spec.Mesh.Namespace).
			Create(context.TODO(), cert, metav1.CreateOptions{})
		if err != nil && !k8serrors.IsAlreadyExists(err) {
			return errors.WithStack(err)
		}
	}

	return nil
}

func EnvoyOAuthSecrets(feature *Feature) error {
	objectMeta := metav1.ObjectMeta{
		Name:      feature.Spec.AppNamespace + "-oauth2-tokens",
		Namespace: feature.Spec.Mesh.Namespace,
		OwnerReferences: []metav1.OwnerReference{
			feature.OwnerReference(),
		},
	}

	envoySecret, err := createEnvoySecret(feature.Spec.OAuth, objectMeta)
	if err != nil {
		return errors.WithStack(err)
	}

	_, err = feature.clientset.CoreV1().
		Secrets(objectMeta.Namespace).
		Create(context.TODO(), envoySecret, metav1.CreateOptions{})
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return errors.WithStack(err)
	}

	return nil
}

func ConfigMaps(feature *Feature) error {
	meshConfig := feature.Spec.Mesh
	if err := feature.createConfigMap("service-mesh-refs",
		map[string]string{
			"CONTROL_PLANE_NAME": meshConfig.Name,
			"MESH_NAMESPACE":     meshConfig.Namespace,
		}); err != nil {
		return errors.WithStack(err)
	}

	authorinoConfig := feature.Spec.Auth.Authorino
	if err := feature.createConfigMap("auth-refs",
		map[string]string{
			"AUTHORINO_LABEL": authorinoConfig.Label,
			"AUTH_AUDIENCE":   strings.Join(authorinoConfig.Audiences, ","),
		}); err != nil {
		return errors.WithStack(err)
	}

	return nil
}

func ServiceMeshEnabledInDashboard(feature *Feature) error {
	gvr := schema.GroupVersionResource{
		Group:    "opendatahub.io",
		Version:  "v1alpha",
		Resource: "odhdashboardconfigs",
	}

	configs, err := feature.dynamicClient.
		Resource(gvr).
		Namespace(feature.Spec.AppNamespace).
		List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	if len(configs.Items) == 0 {
		log.Info("No odhdashboardconfig found in namespace, doing nothing")
		return nil
	}

	// Assuming there is only one odhdashboardconfig in the namespace, patching the first one
	config := configs.Items[0]
	if config.Object["spec"] == nil {
		config.Object["spec"] = map[string]interface{}{}
	}
	spec := config.Object["spec"].(map[string]interface{})
	if spec["dashboardConfig"] == nil {
		spec["dashboardConfig"] = map[string]interface{}{}
	}
	dashboardConfig := spec["dashboardConfig"].(map[string]interface{})
	dashboardConfig["disableServiceMesh"] = false

	if _, err := feature.dynamicClient.Resource(gvr).
		Namespace(feature.Spec.AppNamespace).
		Update(context.Background(), &config, metav1.UpdateOptions{}); err != nil {
		log.Error(err, "Failed to update odhdashboardconfig")

		return err
	}

	log.Info("Successfully patched odhdashboardconfig")

	return nil
}

func MigratedDataScienceProjects(feature *Feature) error {
	selector := labels.SelectorFromSet(labels.Set{"opendatahub.io/dashboard": "true"})

	namespaceClient := feature.clientset.CoreV1().Namespaces()

	namespaces, err := namespaceClient.List(context.TODO(), metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return fmt.Errorf("failed to get namespaces: %v", err)
	}

	var result *multierror.Error

	for _, namespace := range namespaces.Items {
		annotations := namespace.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations["opendatahub.io/service-mesh"] = "true"
		namespace.SetAnnotations(annotations)

		if _, err := namespaceClient.Update(context.TODO(), &namespace, metav1.UpdateOptions{}); err != nil {
			result = multierror.Append(result, err)
		}
	}

	return result.ErrorOrNil()
}
