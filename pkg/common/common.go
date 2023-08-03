package common

import (
	"context"
	"fmt"
	"io/ioutil"
	corev1 "k8s.io/api/core/v1"
	authv1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strings"
)

func UpdatePodSecurityRolebinding(cli client.Client, serviceAccountsList []string, namespace string) error {
	foundRoleBinding := &authv1.RoleBinding{}
	// update default rolebinding created in applications namespace
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
				Namespace: namespace})
		}
	}

	return cli.Update(context.TODO(), foundRoleBinding)
}

func subjectExistInRoleBinding(subjectList []authv1.Subject, serviceAccountName, namespace string) bool {
	for _, subject := range subjectList {
		if subject.Name == serviceAccountName && subject.Namespace == namespace {
			return true
		}
	}
	return false
}

func ReplaceStringsInFile(fileName string, replacements map[string]string) error {
	// Read the contents of the file
	fileContent, err := ioutil.ReadFile(fileName)
	if err != nil {
		return fmt.Errorf("failed to read file: %v", err)
	}

	// Replace all occurrences of the strings in the map
	newContent := string(fileContent)
	for string1, string2 := range replacements {
		newContent = strings.ReplaceAll(newContent, string1, string2)
	}

	// Write the modified content back to the file
	err = ioutil.WriteFile(fileName, []byte(newContent), 0)
	if err != nil {
		return fmt.Errorf("failed to write to file: %v", err)
	}

	return nil
}

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
