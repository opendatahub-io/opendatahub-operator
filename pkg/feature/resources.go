package feature

import (
	"context"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CreateNamespace will create namespace with the given name if it does not exist yet and sets owner, so it will be deleted
// when a feature is cleaned up.
func CreateNamespace(namespace string) Action {
	return func(f *Feature) error {
		nsClient := f.Clientset.CoreV1().Namespaces()

		_, err := nsClient.Get(context.TODO(), namespace, metav1.GetOptions{})
		if k8serrors.IsNotFound(err) {
			_, err := nsClient.Create(context.TODO(), &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
					OwnerReferences: []metav1.OwnerReference{
						f.OwnerReference(),
					},
				},
			}, metav1.CreateOptions{})

			// we either successfully created new namespace or failed during the process
			// returning err which indicates the state
			return err
		}

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
