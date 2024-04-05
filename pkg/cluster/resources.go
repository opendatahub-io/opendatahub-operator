package cluster

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	authv1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UpdatePodSecurityRolebinding update default rolebinding which is created in applications namespace by manifests
// being used by different components.
func UpdatePodSecurityRolebinding(cli client.Client, namespace string, serviceAccountsList ...string) error {
	foundRoleBinding := &authv1.RoleBinding{}
	if err := cli.Get(context.TODO(), client.ObjectKey{Name: namespace, Namespace: namespace}, foundRoleBinding); err != nil {
		return fmt.Errorf("error to get rolebinding %s from namespace %s: %w", namespace, namespace, err)
	}

	for _, sa := range serviceAccountsList {
		// Append serviceAccount if not added already
		if !subjectExistInRoleBinding(foundRoleBinding.Subjects, sa, namespace) {
			foundRoleBinding.Subjects = append(foundRoleBinding.Subjects, authv1.Subject{
				Kind:      authv1.ServiceAccountKind,
				Name:      sa,
				Namespace: namespace,
			})
		}
	}

	if err := cli.Update(context.TODO(), foundRoleBinding); err != nil {
		return fmt.Errorf("error update rolebinding %s with serviceaccount: %w", namespace, err)
	}

	return nil
}

// Internal function used by UpdatePodSecurityRolebinding()
// Return whether Rolebinding matching service account and namespace exists or not.
func subjectExistInRoleBinding(subjectList []authv1.Subject, serviceAccountName, namespace string) bool {
	for _, subject := range subjectList {
		if subject.Name == serviceAccountName && subject.Namespace == namespace {
			return true
		}
	}

	return false
}

// CreateSecret creates secrets required by dashboard component in downstream.
func CreateSecret(cli client.Client, name, namespace string, metaOptions ...MetaOptions) error {
	desiredSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
	}

	if err := ApplyMetaOptions(desiredSecret, metaOptions...); err != nil {
		return err
	}

	foundSecret := &corev1.Secret{}
	err := cli.Get(context.TODO(), client.ObjectKey{Name: name, Namespace: namespace}, foundSecret)
	if err != nil {
		if apierrs.IsNotFound(err) {
			err = cli.Create(context.TODO(), desiredSecret)
			if err != nil && !apierrs.IsAlreadyExists(err) {
				return err
			}
		} else {
			return err
		}
	}

	return nil
}

func CreateOrUpdateConfigMap(c client.Client, name string, namespace string, data map[string]string, metaOptions ...MetaOptions) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: data,
	}

	if err := ApplyMetaOptions(configMap, metaOptions...); err != nil {
		return nil, err
	}

	getErr := c.Get(context.TODO(), client.ObjectKey{
		Name:      name,
		Namespace: namespace,
	}, configMap)

	if getErr != nil {
		if apierrs.IsNotFound(getErr) {
			if err := c.Create(context.TODO(), configMap); err != nil {
				return nil, err
			}
		} else {
			return nil, getErr
		}
	}

	for key, value := range data {
		configMap.Data[key] = value
	}

	return configMap, c.Update(context.TODO(), configMap)
}

// CreateNamespace creates namespace and apply metadata.
// If namespace already exists, it will not apply metadata.
func CreateNamespace(cli client.Client, namespace string, metaOptions ...MetaOptions) (*corev1.Namespace, error) {
	desiredNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}

	if err := ApplyMetaOptions(desiredNamespace, metaOptions...); err != nil {
		return nil, err
	}

	foundNamespace := &corev1.Namespace{}
	err := cli.Get(context.TODO(), client.ObjectKey{Name: namespace}, foundNamespace)
	if err != nil {
		if apierrs.IsNotFound(err) {
			err = cli.Create(context.TODO(), desiredNamespace)
			if err != nil && !apierrs.IsAlreadyExists(err) {
				return nil, err
			}
			desiredNamespace.DeepCopyInto(foundNamespace)
		} else {
			return nil, err
		}
	}

	return foundNamespace, nil
}
