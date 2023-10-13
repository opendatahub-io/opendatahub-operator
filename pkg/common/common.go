/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package common contains utility functions used by different components
package common

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	kfdefv1 "github.com/opendatahub-io/opendatahub-operator/apis/kfdef.apps.kubeflow.org/v1"
	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	ofapi "github.com/operator-framework/api/pkg/operators/v1alpha1"
	olmclientset "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned/typed/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	authv1 "k8s.io/api/rbac/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// DeleteConfigMapLabel is the label for configMap used to trigger operator uninstall
	// TODO: Label should be updated if addon name changes
	DeleteConfigMapLabel = "api.openshift.com/addon-managed-odh-delete"
	// odhGeneratedNamespaceLabel is the label added to all the namespaces genereated by odh-deployer
	odhGeneratedNamespaceLabel = "opendatahub.io/generated-namespace"
)

// UpdatePodSecurityRolebinding update default rolebinding which is created in applications namespace by manifests
// being used by different components.
func UpdatePodSecurityRolebinding(cli client.Client, serviceAccountsList []string, namespace string) error {
	foundRoleBinding := &authv1.RoleBinding{}
	err := cli.Get(context.TODO(), client.ObjectKey{Name: namespace, Namespace: namespace}, foundRoleBinding)
	if err != nil {
		return err
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

	return cli.Update(context.TODO(), foundRoleBinding)
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

// ReplaceStringsInFile replaces variable with value in manifests during runtime.
func ReplaceStringsInFile(fileName string, replacements map[string]string) error {
	// Read the contents of the file
	fileContent, err := os.ReadFile(fileName)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Replace all occurrences of the strings in the map
	newContent := string(fileContent)
	for string1, string2 := range replacements {
		newContent = strings.ReplaceAll(newContent, string1, string2)
	}

	// Write the modified content back to the file
	err = os.WriteFile(fileName, []byte(newContent), 0)
	if err != nil {
		return fmt.Errorf("failed to write to file: %w", err)
	}

	return nil
}

// CreateSecret creates secrets required by dashboard component in downstream.
func CreateSecret(cli client.Client, name, namespace string) error {
	desiredSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Type: corev1.SecretTypeOpaque,
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

// CreateNamespace creates namespace required by workbenches component in downstream.
func CreateNamespace(cli client.Client, namespace string) error {
	desiredNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
			Labels: map[string]string{
				odhGeneratedNamespaceLabel: "true",
			},
		},
	}

	foundNamespace := &corev1.Namespace{}
	err := cli.Get(context.TODO(), client.ObjectKey{Name: namespace}, foundNamespace)
	if err != nil {
		if apierrs.IsNotFound(err) {
			err = cli.Create(context.TODO(), desiredNamespace)
			if err != nil && !apierrs.IsAlreadyExists(err) {
				return err
			}
		} else {
			return err
		}
	}
	return nil
}

func GetOperatorNamespace() (string, error) {
	operatorNs := "openshift-operators"
	data, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err == nil {
		if ns := strings.TrimSpace(string(data)); len(ns) > 0 {
			operatorNs = ns
			return operatorNs, nil
		}
	}
	return "", err
}

func RemoveKfDefInstances(cli client.Client) error {
	// Check if kfdef are deployed
	kfdefCrd := &apiextv1.CustomResourceDefinition{}

	err := cli.Get(context.TODO(), client.ObjectKey{Name: "kfdefs.kfdef.apps.kubeflow.org"}, kfdefCrd)
	if err != nil {
		if apierrs.IsNotFound(err) {
			// If no Crd found, return, since its a new Installation
			return nil
		} else {
			return fmt.Errorf("error retrieving kfdef CRD : %v", err)
		}
	} else {
		expectedKfDefList := &kfdefv1.KfDefList{}
		err := cli.List(context.TODO(), expectedKfDefList)
		if err != nil {
			if apierrs.IsNotFound(err) {
				// If no KfDefs, do nothing and return
				return nil
			} else {
				return fmt.Errorf("error getting list of kfdefs: %v", err)
			}
		}
		// Delete kfdefs
		for _, kfdef := range expectedKfDefList.Items {
			// Remove finalizer
			updatedKfDef := &kfdef
			updatedKfDef.Finalizers = []string{}
			err = cli.Update(context.TODO(), updatedKfDef)
			if err != nil {
				return fmt.Errorf("error removing finalizers from kfdef %v : %v", kfdef.Name, err)
			}
			err = cli.Delete(context.TODO(), updatedKfDef)
			if err != nil {
				return fmt.Errorf("error deleting kfdef %v : %v", kfdef.Name, err)
			}
		}
	}
	return nil
}

func removeCsv(c client.Client, r *rest.Config) error {
	// Get watchNamespace
	operatorNamespace, err := GetOperatorNamespace()
	if err != nil {
		return err
	}

	operatorCsv, err := getClusterServiceVersion(r, operatorNamespace)
	if err != nil {
		return err
	}

	if operatorCsv != nil {
		fmt.Printf("Deleting csv %s", operatorCsv.Name)
		err = c.Delete(context.TODO(), operatorCsv, []client.DeleteOption{}...)
		if err != nil {
			if apierrs.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("error deleting clusterserviceversion: %v", err)
		}
		fmt.Printf("Clusterserviceversion %s deleted as a part of uninstall.", operatorCsv.Name)
	}
	fmt.Printf("No clusterserviceversion for the operator found.")
	return nil
}

// getClusterServiceVersion retries the clusterserviceversions available in the operator namespace.
func getClusterServiceVersion(cfg *rest.Config, watchNameSpace string) (*ofapi.ClusterServiceVersion, error) {

	operatorClient, err := olmclientset.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("error getting operator client %v", err)
	}
	csvs, err := operatorClient.ClusterServiceVersions(watchNameSpace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	// get csv with CRD DataScienceCluster
	if len(csvs.Items) != 0 {
		for _, csv := range csvs.Items {
			for _, operatorCR := range csv.Spec.CustomResourceDefinitions.Owned {
				if operatorCR.Kind == "DataScienceCluster" {
					return &csv, nil
				}
			}
		}
	}
	return nil, nil
}

// OperatorUninstall deletes all the externally generated resources. This includes monitoring resources and applications
// installed by KfDef.
func OperatorUninstall(cli client.Client, cfg *rest.Config) error {

	// Delete kfdefs if found
	err := RemoveKfDefInstances(cli)
	if err != nil {
		return err
	}

	// Delete DSCInitialization instance
	err = removeDSCInitialization(cli)
	if err != nil {
		return err
	}
	// Delete generated namespaces by the operator
	generatedNamespaces := &corev1.NamespaceList{}
	nsOptions := []client.ListOption{
		client.MatchingLabels{odhGeneratedNamespaceLabel: "true"},
	}
	if err := cli.List(context.TODO(), generatedNamespaces, nsOptions...); err != nil {
		if !apierrs.IsNotFound(err) {
			return fmt.Errorf("error getting generated namespaces : %v", err)
		}
	}

	// Return if any one of the namespaces is Terminating due to resources that are in process of deletion. (e.g CRDs)
	if len(generatedNamespaces.Items) != 0 {
		for _, namespace := range generatedNamespaces.Items {
			if namespace.Status.Phase == corev1.NamespaceTerminating {
				return fmt.Errorf("waiting for namespace %v to be deleted", namespace.Name)
			}
		}
	}

	// Delete all the active namespaces
	for _, namespace := range generatedNamespaces.Items {
		if namespace.Status.Phase == corev1.NamespaceActive {
			if err := cli.Delete(context.TODO(), &namespace, []client.DeleteOption{}...); err != nil {
				return fmt.Errorf("error deleting namespace %v: %v", namespace.Name, err)
			}
			fmt.Printf("Namespace %s deleted as a part of uninstall.", namespace.Name)
		}
	}

	// Wait for all resources to get cleaned up
	time.Sleep(10 * time.Second)
	fmt.Printf("All resources deleted as part of uninstall. Removing the operator csv")
	return removeCsv(cli, cfg)
}

func removeDSCInitialization(cli client.Client) error {
	// Last check if multiple instances of DSCInitialization exist
	instanceList := &dsci.DSCInitializationList{}
	var err error
	err = cli.List(context.TODO(), instanceList)
	if err != nil {
		return err
	}

	if len(instanceList.Items) != 0 {
		for _, dsciInstance := range instanceList.Items {
			err = cli.Delete(context.TODO(), &dsciInstance)
			if apierrs.IsNotFound(err) {
				err = nil
			}
		}
	}
	return err
}

// HasDeleteConfigMap returns true if delete configMap is added to the operator namespace by managed-tenants repo.
// It returns false in all other cases.
func HasDeleteConfigMap(c client.Client) bool {
	// Get watchNamespace
	operatorNamespace, err := GetOperatorNamespace()
	if err != nil {
		return false
	}

	// If delete configMap is added, uninstall the operator and the resources
	deleteConfigMapList := &corev1.ConfigMapList{}
	cmOptions := []client.ListOption{
		client.InNamespace(operatorNamespace),
		client.MatchingLabels{DeleteConfigMapLabel: "true"},
	}

	if err := c.List(context.TODO(), deleteConfigMapList, cmOptions...); err != nil {
		return false
	}
	return len(deleteConfigMapList.Items) != 0
}
