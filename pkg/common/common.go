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
	"crypto/sha256"
	b64 "encoding/base64"
	"fmt"
	"os"
	"regexp"
	"strings"

	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	authv1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// odhGeneratedNamespaceLabel is the label added to all the namespaces genereated by odh-deployer
	odhGeneratedNamespaceLabel = "opendatahub.io/generated-namespace"
)

// UpdatePodSecurityRolebinding update default rolebinding which is created in namespace by manifests
// being used by different components and sre monitoring
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

// MatchLineInFile use the 'key' of the replacements as match pattern and replace the line with 'value'
func MatchLineInFile(fileName string, replacements map[string]string) error {
	fileContent, err := os.ReadFile(fileName)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}
	newContent := string(fileContent)
	for matchPattern, NewValue := range replacements {
		re := regexp.MustCompile(matchPattern + `(.*)`)
		newContent = re.ReplaceAllString(newContent, NewValue)
	}
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

// encode configmap data and return in base64
func GetMonitoringData(data string) (string, error) {
	// Create a new SHA-256 hash object
	hash := sha256.New()

	// Write the input data to the hash object
	_, err := hash.Write([]byte(data))
	if err != nil {
		return "", err
	}

	// Get the computed hash sum
	hashSum := hash.Sum(nil)

	// Encode the hash sum to Base64
	encodedData := b64.StdEncoding.EncodeToString(hashSum)

	return encodedData, nil
}

// Use openshift-console namespace to get host domain
func GetDomain(cli client.Client, name string, namespace string) (string, error) {
	consoleRoute := &routev1.Route{}
	if err := cli.Get(context.TODO(), client.ObjectKey{Name: name, Namespace: namespace}, consoleRoute); err != nil {
		return "", fmt.Errorf("error getting %s route URL: %w", name, err)
	}
	domainIndex := strings.Index(consoleRoute.Spec.Host, ".")
	return consoleRoute.Spec.Host[domainIndex+1:], nil
}
