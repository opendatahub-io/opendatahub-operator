package feature

import (
	"context"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/pkg/errors"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CreateNamespace will create namespace with the given name if it does not exist yet and sets feature as an owner of it.
// This way we ensure that when the feature is cleaned up, the namespace will be deleted as well.
func CreateNamespace(namespace string) Action {
	return func(f *Feature) error {
		createdNs, err := cluster.CreateNamespace(f.Client, namespace)
		if err != nil {
			return err
		}

		createdNs.SetOwnerReferences([]metav1.OwnerReference{f.OwnerReference()})

		nsClient := f.Clientset.CoreV1().Namespaces()
		_, err = nsClient.Update(context.TODO(), createdNs, metav1.UpdateOptions{})

		return err
	}
}

func SelfSignedCertificate(feature *Feature) error {
	if feature.Spec.Mesh.Certificate.Generate {
		meta := metav1.ObjectMeta{
			Name:      feature.Spec.Mesh.Certificate.Name,
			Namespace: feature.Spec.Mesh.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				feature.OwnerReference(),
			},
		}

		cert, err := GenerateSelfSignedCertificateAsSecret(feature.Spec.Domain, meta)
		if err != nil {
			return errors.WithStack(err)
		}

		if err != nil {
			return errors.WithStack(err)
		}

		_, err = feature.Clientset.CoreV1().
			Secrets(feature.Spec.Mesh.Namespace).
			Create(context.TODO(), cert, metav1.CreateOptions{})
		if err != nil && !k8serrors.IsAlreadyExists(err) {
			return errors.WithStack(err)
		}
	}

	return nil
}
