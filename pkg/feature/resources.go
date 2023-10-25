package feature

import (
	"context"
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
