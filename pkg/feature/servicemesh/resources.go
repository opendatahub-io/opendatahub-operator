package servicemesh

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/gvr"
)

func SelfSignedCertificate(f *feature.Feature) error {
	if f.Spec.Mesh.Certificate.Generate {
		meta := metav1.ObjectMeta{
			Name:      f.Spec.Mesh.Certificate.Name,
			Namespace: f.Spec.Mesh.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				f.OwnerReference(),
			},
		}

		cert, err := feature.GenerateSelfSignedCertificateAsSecret(f.Spec.Domain, meta)
		if err != nil {
			return errors.WithStack(err)
		}

		if err != nil {
			return errors.WithStack(err)
		}

		_, err = f.Clientset.CoreV1().
			Secrets(f.Spec.Mesh.Namespace).
			Create(context.TODO(), cert, metav1.CreateOptions{})
		if err != nil && !k8serrors.IsAlreadyExists(err) {
			return errors.WithStack(err)
		}
	}

	return nil
}

func EnvoyOAuthSecrets(feature *feature.Feature) error {
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

	_, err = feature.Clientset.CoreV1().
		Secrets(objectMeta.Namespace).
		Create(context.TODO(), envoySecret, metav1.CreateOptions{})
	if err != nil && !k8serrors.IsAlreadyExists(err) {
		return errors.WithStack(err)
	}

	return nil
}

func ConfigMaps(feature *feature.Feature) error {
	meshConfig := feature.Spec.Mesh
	if err := feature.CreateConfigMap("service-mesh-refs",
		map[string]string{
			"CONTROL_PLANE_NAME": meshConfig.Name,
			"MESH_NAMESPACE":     meshConfig.Namespace,
		}); err != nil {
		return errors.WithStack(err)
	}

	authorinoConfig := feature.Spec.Auth.Authorino
	if err := feature.CreateConfigMap("auth-refs",
		map[string]string{
			"AUTHORINO_LABEL": authorinoConfig.Label,
			"AUTH_AUDIENCE":   strings.Join(authorinoConfig.Audiences, ","),
		}); err != nil {
		return errors.WithStack(err)
	}

	return nil
}

func EnabledInDashboard(feature *feature.Feature) error {
	return setServiceMeshDisabledFlag(false)(feature)
}

func DisabledInDashboard(feature *feature.Feature) error {
	return setServiceMeshDisabledFlag(true)(feature)
}

func setServiceMeshDisabledFlag(disabled bool) feature.Action {
	return func(feature *feature.Feature) error {
		configs, err := feature.DynamicClient.
			Resource(gvr.ODHDashboardConfigGVR).
			Namespace(feature.Spec.AppNamespace).
			List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return err
		}

		if len(configs.Items) == 0 {
			log.Info("No odhdashboardconfig found in namespace, doing nothing", "name", feature.Name)
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
		dashboardConfig["disableServiceMesh"] = disabled

		if _, err := feature.DynamicClient.Resource(gvr.ODHDashboardConfigGVR).
			Namespace(feature.Spec.AppNamespace).
			Update(context.TODO(), &config, metav1.UpdateOptions{}); err != nil {
			log.Error(err, "Failed to update odhdashboardconfig", "name", feature.Name)

			return err
		}

		log.Info("Successfully patched odhdashboardconfig", "name", feature.Name)
		return nil
	}
}

func MigratedDataScienceProjects(feature *feature.Feature) error {
	selector := labels.SelectorFromSet(labels.Set{"opendatahub.io/dashboard": "true"})

	namespaceClient := feature.Clientset.CoreV1().Namespaces()

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
