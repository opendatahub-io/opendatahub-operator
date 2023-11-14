package feature

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

// CreateNamespace will create namespace with the given name if it does not exist yet and sets feature as an owner of it.
// This way we ensure that when the feature is cleaned up, the namespace will be deleted as well. If the namespace
// already exists, no action will be performed.
func CreateNamespace(namespace string) Action {
	return func(f *Feature) error {
		// Despite the cluster.CreateNamespace function already checks if the target
		// namespace exists, it seems relevant to do the check here. Otherwise, we may
		// set or change the owner reference of an existent namespace, and that would lead
		// to namespace deletion for cases where it is better to not terminate it.
		foundNamespace := &corev1.Namespace{}
		err := f.Client.Get(context.TODO(), client.ObjectKey{Name: namespace}, foundNamespace)
		if err != nil {
			if !apierrs.IsNotFound(err) {
				return err
			}
		} else {
			// Namespace exists. We do no-op.
			return nil
		}

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
