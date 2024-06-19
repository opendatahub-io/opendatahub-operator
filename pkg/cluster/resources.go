package cluster

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	authv1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

// UpdatePodSecurityRolebinding update default rolebinding which is created in applications namespace by manifests
// being used by different components and SRE monitoring.
func UpdatePodSecurityRolebinding(ctx context.Context, cli client.Client, namespace string, serviceAccountsList ...string) error {
	foundRoleBinding := &authv1.RoleBinding{}
	if err := cli.Get(ctx, client.ObjectKey{Name: namespace, Namespace: namespace}, foundRoleBinding); err != nil {
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

	if err := cli.Update(ctx, foundRoleBinding); err != nil {
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
func CreateSecret(ctx context.Context, cli client.Client, name, namespace string, metaOptions ...MetaOptions) error {
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
	err := cli.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, foundSecret)
	if err != nil {
		if apierrs.IsNotFound(err) {
			err = cli.Create(ctx, desiredSecret)
			if err != nil && !apierrs.IsAlreadyExists(err) {
				return err
			}
		} else {
			return err
		}
	}
	return nil
}

// CreateOrUpdateConfigMap creates a new configmap or updates an existing one.
// If the configmap already exists, it will be updated with the merged Data and MetaOptions, if any.
// ConfigMap.ObjectMeta.Name and ConfigMap.ObjectMeta.Namespace are both required, it returns an error otherwise.
func CreateOrUpdateConfigMap(ctx context.Context, c client.Client, desiredCfgMap *corev1.ConfigMap, metaOptions ...MetaOptions) error {
	if desiredCfgMap.GetName() == "" || desiredCfgMap.GetNamespace() == "" {
		return fmt.Errorf("configmap name and namespace must be set")
	}

	existingCfgMap := &corev1.ConfigMap{}
	err := c.Get(ctx, client.ObjectKey{
		Name:      desiredCfgMap.Name,
		Namespace: desiredCfgMap.Namespace,
	}, existingCfgMap)

	if apierrs.IsNotFound(err) {
		if applyErr := ApplyMetaOptions(desiredCfgMap, metaOptions...); applyErr != nil {
			return applyErr
		}
		return c.Create(ctx, desiredCfgMap)
	} else if err != nil {
		return err
	}

	if applyErr := ApplyMetaOptions(existingCfgMap, metaOptions...); applyErr != nil {
		return applyErr
	}

	if existingCfgMap.Data == nil {
		existingCfgMap.Data = make(map[string]string)
	}
	for key, value := range desiredCfgMap.Data {
		existingCfgMap.Data[key] = value
	}

	if updateErr := c.Update(ctx, existingCfgMap); updateErr != nil {
		return updateErr
	}

	existingCfgMap.DeepCopyInto(desiredCfgMap)
	return nil
}

// CreateNamespace creates a namespace and apply metadata.
// If a namespace already exists, the operation has no effect on it.
func CreateNamespace(ctx context.Context, cli client.Client, namespace string, metaOptions ...MetaOptions) (*corev1.Namespace, error) {
	desiredNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}

	if err := ApplyMetaOptions(desiredNamespace, metaOptions...); err != nil {
		return nil, err
	}

	foundNamespace := &corev1.Namespace{}
	if getErr := cli.Get(ctx, client.ObjectKey{Name: namespace}, foundNamespace); client.IgnoreNotFound(getErr) != nil {
		return nil, getErr
	}

	createErr := cli.Create(ctx, desiredNamespace)
	if apierrs.IsAlreadyExists(createErr) {
		return foundNamespace, nil
	}

	return desiredNamespace, client.IgnoreAlreadyExists(createErr)
}

// WaitForDeploymentAvailable to check if component deployment from 'namespace' is ready within 'timeout' before apply prometheus rules for the component.
func WaitForDeploymentAvailable(ctx context.Context, c client.Client, componentName string, namespace string, interval int, timeout int) error {
	resourceInterval := time.Duration(interval) * time.Second
	resourceTimeout := time.Duration(timeout) * time.Minute

	return wait.PollUntilContextTimeout(ctx, resourceInterval, resourceTimeout, true, func(ctx context.Context) (bool, error) {
		componentDeploymentList := &v1.DeploymentList{}
		err := c.List(ctx, componentDeploymentList, client.InNamespace(namespace), client.HasLabels{labels.ODH.Component(componentName)})
		if err != nil {
			return false, fmt.Errorf("error fetching list of deployments: %w", err)
		}

		fmt.Printf("waiting for %d deployment to be ready for %s\n", len(componentDeploymentList.Items), componentName)
		for _, deployment := range componentDeploymentList.Items {
			if deployment.Status.ReadyReplicas != deployment.Status.Replicas {
				return false, nil
			}
		}

		return true, nil
	})
}

func CreateWithRetry(ctx context.Context, cli client.Client, obj client.Object, timeoutMin int) error {
	interval := time.Second * 5 // arbitrary value
	timeout := time.Duration(timeoutMin) * time.Minute

	return wait.PollUntilContextTimeout(ctx, interval, timeout, true, func(ctx context.Context) (bool, error) {
		err := cli.Create(ctx, obj)
		if err != nil {
			return false, nil //nolint:nilerr
		}
		return true, nil
	})
}
