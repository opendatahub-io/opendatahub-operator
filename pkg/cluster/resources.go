package cluster

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

// UpdatePodSecurityRolebinding update default rolebinding which is created in applications namespace by manifests
// being used by different components and SRE monitoring.
func UpdatePodSecurityRolebinding(ctx context.Context, cli client.Client, namespace string, serviceAccountsList ...string) error {
	foundRoleBinding := &rbacv1.RoleBinding{}
	if err := cli.Get(ctx, client.ObjectKey{Name: namespace, Namespace: namespace}, foundRoleBinding); err != nil {
		return fmt.Errorf("error to get rolebinding %s from namespace %s: %w", namespace, namespace, err)
	}

	for _, sa := range serviceAccountsList {
		// Append serviceAccount if not added already
		if !SubjectExistInRoleBinding(foundRoleBinding.Subjects, sa, namespace) {
			foundRoleBinding.Subjects = append(foundRoleBinding.Subjects, rbacv1.Subject{
				Kind:      rbacv1.ServiceAccountKind,
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

// SubjectExistInRoleBinding return whether RoleBinding matching service account and namespace exists or not.
func SubjectExistInRoleBinding(subjectList []rbacv1.Subject, serviceAccountName, namespace string) bool {
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
	err := cli.Get(ctx, client.ObjectKeyFromObject(desiredSecret), foundSecret)
	if err != nil {
		if k8serr.IsNotFound(err) {
			err = cli.Create(ctx, desiredSecret)
			if err != nil && !k8serr.IsAlreadyExists(err) {
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
	if applyErr := ApplyMetaOptions(desiredCfgMap, metaOptions...); applyErr != nil {
		return applyErr
	}

	if desiredCfgMap.GetName() == "" || desiredCfgMap.GetNamespace() == "" {
		return errors.New("configmap name and namespace must be set")
	}

	existingCfgMap := &corev1.ConfigMap{}
	err := c.Get(ctx, client.ObjectKeyFromObject(desiredCfgMap), existingCfgMap)
	if k8serr.IsNotFound(err) {
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
	if getErr := cli.Get(ctx, client.ObjectKeyFromObject(desiredNamespace), foundNamespace); client.IgnoreNotFound(getErr) != nil {
		return nil, getErr
	}

	createErr := cli.Create(ctx, desiredNamespace)
	if k8serr.IsAlreadyExists(createErr) {
		return foundNamespace, nil
	}

	return desiredNamespace, client.IgnoreAlreadyExists(createErr)
}

// ExecuteOnAllNamespaces executes the passed function for all namespaces in the cluster retrieved in batches.
func ExecuteOnAllNamespaces(ctx context.Context, cli client.Client, processFunc func(*corev1.Namespace) error) error {
	namespaces := &corev1.NamespaceList{}
	paginateListOption := &client.ListOptions{
		Limit: 500,
	}

	for { // loop over all paged results
		if err := cli.List(ctx, namespaces, paginateListOption); err != nil {
			return err
		}
		for i := range namespaces.Items {
			ns := &namespaces.Items[i]
			if err := processFunc(ns); err != nil {
				return err
			}
		}
		if paginateListOption.Continue = namespaces.GetContinue(); namespaces.GetContinue() == "" {
			break
		}
	}
	return nil
}

// WaitForDeploymentAvailable to check if component deployment from 'namespace' is ready within 'timeout' before apply prometheus rules for the component.
func WaitForDeploymentAvailable(ctx context.Context, c client.Client, componentName string, namespace string, interval int, timeout int) error {
	log := logf.FromContext(ctx)
	resourceInterval := time.Duration(interval) * time.Second
	resourceTimeout := time.Duration(timeout) * time.Minute

	return wait.PollUntilContextTimeout(ctx, resourceInterval, resourceTimeout, true, func(ctx context.Context) (bool, error) {
		componentDeploymentList := &appsv1.DeploymentList{}
		err := c.List(ctx, componentDeploymentList, client.InNamespace(namespace), client.HasLabels{labels.ODH.Component(componentName)})
		if err != nil {
			return false, fmt.Errorf("error fetching list of deployments: %w", err)
		}

		log.Info("waiting for " + strconv.Itoa(len(componentDeploymentList.Items)) + " deployment to be ready for " + componentName)
		for _, deployment := range componentDeploymentList.Items {
			if deployment.Status.ReadyReplicas != deployment.Status.Replicas {
				return false, nil
			}
		}

		return true, nil
	})
}

func CreateWithRetry(ctx context.Context, cli client.Client, obj client.Object, timeoutMin int) error {
	log := logf.FromContext(ctx)
	interval := time.Second * 5 // arbitrary value
	timeout := time.Duration(timeoutMin) * time.Minute

	return wait.PollUntilContextTimeout(ctx, interval, timeout, true, func(ctx context.Context) (bool, error) {
		// Create can return:
		// If webhook enabled:
		//   - no error (err == nil)
		//   - 500 InternalError likely if webhook is not available (yet)
		//   - 403 Forbidden if webhook blocks creation (check of existence)
		//   - some problem (real error)
		// else, if webhook disabled:
		//   - no error (err == nil)
		//   - 409 AlreadyExists if object exists
		//   - some problem (real error)
		errCreate := cli.Create(ctx, obj)
		if errCreate == nil {
			return true, nil
		}

		// check existence, success case for the function, covers 409 and 403 (or newly created)
		errGet := cli.Get(ctx, client.ObjectKeyFromObject(obj), obj)
		if errGet == nil {
			return true, nil
		}

		// retry if 500, assume webhook is not available
		if k8serr.IsInternalError(errCreate) {
			log.Info("Error creating object, retrying...", "reason", errCreate)
			return false, nil
		}

		// some other error
		return false, errCreate
	})
}
