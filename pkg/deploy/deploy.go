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

// Package deploy
package deploy

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	ofapiv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	ofapiv2 "github.com/operator-framework/api/pkg/operators/v2"
	"golang.org/x/exp/maps"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/resmap"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/plugins"
)

const (
	DefaultManifestPath = "/opt/manifests"
)

// DownloadManifests function performs following tasks:
// 1. It takes component URI and only downloads folder specified by component.ContextDir field
// 2. It saves the manifests in the odh-manifests/component-name/ folder.
func DownloadManifests(componentName string, manifestConfig components.ManifestsConfig) error {
	// Get the component repo from the given url
	// e.g.  https://github.com/example/tarball/master
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, manifestConfig.URI, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("error downloading manifests: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("error downloading manifests: %v HTTP status", resp.StatusCode)
	}

	// Create a new gzip reader
	gzipReader, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("error creating gzip reader: %w", err)
	}
	defer gzipReader.Close()

	// Create a new TAR reader
	tarReader := tar.NewReader(gzipReader)

	// Create manifest directory
	mode := os.ModePerm
	err = os.MkdirAll(DefaultManifestPath, mode)
	if err != nil {
		return fmt.Errorf("error creating manifests directory : %w", err)
	}

	// Extract the contents of the TAR archive to the current directory
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}

		componentFiles := strings.Split(header.Name, "/")
		componentFileName := header.Name
		componentManifestPath := componentFiles[0] + "/" + manifestConfig.ContextDir

		if strings.Contains(componentFileName, componentManifestPath) {
			// Get manifest path relative to repo
			// e.g. of repo/a/b/manifests/base --> base/
			componentFoldersList := strings.Split(componentFileName, "/")
			componentFileRelativePathFound := strings.Join(componentFoldersList[len(strings.Split(componentManifestPath, "/")):], "/")

			if header.Typeflag == tar.TypeDir {
				err = os.MkdirAll(DefaultManifestPath+"/"+componentName+"/"+componentFileRelativePathFound, mode)
				if err != nil {
					return fmt.Errorf("error creating directory:%w", err)
				}

				continue
			}

			if header.Typeflag == tar.TypeReg {
				file, err := os.Create(DefaultManifestPath + "/" + componentName + "/" + componentFileRelativePathFound)
				if err != nil {
					fmt.Println("Error creating file:", err)
				}
				for {
					_, err := io.CopyN(file, tarReader, 1024)
					if err != nil {
						if errors.Is(err, io.EOF) {
							break
						}
						fmt.Println("Error downloading file contents:", err)
						return err
					}
				}
				file.Close()

				continue
			}
		}
	}

	return err
}

func DeployManifestsFromPath(cli client.Client, owner metav1.Object, manifestPath string, namespace string, componentName string, componentEnabled bool) error { //nolint:golint,revive,lll
	// Render the Kustomize manifests
	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	fs := filesys.MakeFsOnDisk()
	// Create resmap
	// Use kustomization file under manifestPath or use `default` overlay
	var resMap resmap.ResMap
	_, err := os.Stat(filepath.Join(manifestPath, "kustomization.yaml"))
	if err != nil {
		if os.IsNotExist(err) {
			resMap, err = k.Run(fs, filepath.Join(manifestPath, "default"))
		}
	} else {
		resMap, err = k.Run(fs, manifestPath)
	}

	if err != nil {
		return err
	}

	// Apply NamespaceTransformer Plugin
	if err := plugins.ApplyNamespacePlugin(namespace, resMap); err != nil {
		return err
	}

	// Apply LabelTransformer Plugin
	if err := plugins.ApplyAddLabelsPlugin(componentName, resMap); err != nil {
		return err
	}

	objs, err := GetResources(resMap)
	if err != nil {
		return err
	}
	// Create / apply / delete resources in the cluster
	for _, obj := range objs {
		err = manageResource(context.TODO(), cli, obj, owner, namespace, componentName, componentEnabled)
		if err != nil {
			return err
		}
	}

	return nil
}

func GetResources(resMap resmap.ResMap) ([]*unstructured.Unstructured, error) {
	resources := make([]*unstructured.Unstructured, 0, resMap.Size())
	for _, res := range resMap.Resources() {
		u := &unstructured.Unstructured{}
		err := yaml.Unmarshal([]byte(res.MustYaml()), u)
		if err != nil {
			return nil, err
		}
		resources = append(resources, u)
	}

	return resources, nil
}

func manageResource(ctx context.Context, cli client.Client, obj *unstructured.Unstructured, owner metav1.Object, applicationNamespace, componentName string, enabled bool) error {
	resourceName := obj.GetName()
	namespace := obj.GetNamespace()

	found := obj.DeepCopy()

	err := cli.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: namespace}, found)

	// Return if resource is of Kind: Namespace and Name: odhApplicationsNamespace
	if obj.GetKind() == "Namespace" && obj.GetName() == applicationNamespace {
		return nil
	}

	// Resource exists but component is disabled
	if !enabled {
		if err != nil {
			return nil //nolint:nilerr // Return nil for any errors getting the resource, since the component itself is disabled
		}

		// Check for shared resources before deletion
		resourceLabels := found.GetLabels()
		var componentCounter []string
		if resourceLabels != nil {
			for i := range resourceLabels {
				if strings.Contains(i, labels.ODHAppPrefix) {
					compFound := strings.Split(i, "/")[1]
					componentCounter = append(componentCounter, compFound)
				}
			}
			// Shared resource , do not delete. Remove label from disabled component
			if len(componentCounter) > 1 || (len(componentCounter) == 1 && componentCounter[0] != componentName) {
				found.SetLabels(resourceLabels)
				// return, do not delete the shared resource
				return nil
			}

			// Do not delete CRDs, as those can be used by non-odh components
			if found.GetKind() == "CustomResourceDefinition" {
				return nil
			}
		}

		existingOwnerReferences := obj.GetOwnerReferences()
		selector := labels.ODH.Component(componentName)
		// only removed the resource with our label applied, not the same name resource maually created by user
		if existingOwnerReferences == nil && resourceLabels[selector] == "true" {
			return cli.Delete(ctx, found)
		}

		found.SetOwnerReferences([]metav1.OwnerReference{})
		data, err := json.Marshal(found)
		if err != nil {
			return err
		}

		err = cli.Patch(ctx, found, client.RawPatch(types.ApplyPatchType, data), client.ForceOwnership, client.FieldOwner(owner.GetName()))
		if err != nil {
			return err
		}

		return cli.Delete(ctx, found)
	}

	// Create the resource if it doesn't exist and component is enabled
	if apierrs.IsNotFound(err) {
		// Set the owner reference for garbage collection
		// Skip set on CRD, e.g. we should not delete notebook CRD if we delete DSC instance
		// Skip on OdhDashboardConfig CR, because we want user to be able to update it
		if found.GetKind() != "CustomResourceDefinition" && found.GetKind() != "OdhDashboardConfig" {
			if err = ctrl.SetControllerReference(owner, metav1.Object(obj), cli.Scheme()); err != nil {
				return err
			}
		}

		return cli.Create(ctx, obj)
	}

	// Exception: ODHDashboardConfig should not be updated even with upgrades
	// TODO: Move this out when we have dashboard-controller
	if found.GetKind() == "OdhDashboardConfig" {
		// Do nothing, return
		return nil
	}

	// Kserve specific workflow:
	// TODO: Remove this when we have generalize custom config requirements across all components
	if componentName == "kserve" {
		// do not reconcile kserve resource with annotation "opendatahub.io/managed: false"
		if found.GetAnnotations()["opendatahub.io/managed"] == "false" {
			return nil
		}
		// do not patch resources field in Kserve deployment i.e allows users to update resources field
		if err := removeResourcesFromDeployment(obj); err != nil {
			return err
		}
	}

	// Preserve app.opendatahub.io/<component> labels of previous versions of existing objects
	foundLabels := make(map[string]string)
	for k, v := range found.GetLabels() {
		if strings.Contains(k, labels.ODHAppPrefix) {
			foundLabels[k] = v
		}
	}
	newLabels := obj.GetLabels()
	maps.Copy(foundLabels, newLabels)
	obj.SetLabels(foundLabels)

	// Perform server-side apply
	data, err := json.Marshal(obj)
	if err != nil {
		return err
	}

	return cli.Patch(ctx, found, client.RawPatch(types.ApplyPatchType, data), client.ForceOwnership, client.FieldOwner(owner.GetName()))
}

/*
User env variable passed from CSV (if it is set) to overwrite values from manifests' params.env file
This is useful for air gapped cluster
priority of image values (from high to low):
- image values set in manifests params.env if manifestsURI is set
- RELATED_IMAGE_* values from CSV
- image values set in manifests params.env if manifestsURI is not set.
parameter isUpdateNamespace is used to set if should update namespace  with dsci applicationnamespace.
*/
func ApplyParams(componentPath string, imageParamsMap map[string]string, isUpdateNamespace bool) error {
	envFilePath := filepath.Join(componentPath, "params.env")
	// Require params.env at the root folder
	file, err := os.Open(envFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			// params.env doesn't exist, do not apply any changes
			return nil
		}

		return err
	}
	backupPath := envFilePath + ".bak"
	defer file.Close()

	envMap := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			envMap[parts[0]] = parts[1]
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	// Update images with env variables
	// e.g "odh-kuberay-operator-controller-image": "RELATED_IMAGE_ODH_KUBERAY_OPERATOR_CONTROLLER_IMAGE",
	for i := range envMap {
		relatedImageValue := os.Getenv(imageParamsMap[i])
		if relatedImageValue != "" {
			envMap[i] = relatedImageValue
		}
	}

	// Update namespace variable with applicationNamepsace
	if isUpdateNamespace {
		envMap["namespace"] = imageParamsMap["namespace"]
	}

	// Move the existing file to a backup file and create empty file
	if err := os.Rename(envFilePath, backupPath); err != nil {
		return err
	}

	file, err = os.Create(envFilePath)
	if err != nil {
		// If create fails, try to restore the backup file
		_ = os.Rename(backupPath, envFilePath)

		return err
	}
	defer file.Close()

	// Now, write the map back to the file
	writer := bufio.NewWriter(file)
	for key, value := range envMap {
		if _, fErr := fmt.Fprintf(writer, "%s=%s\n", key, value); fErr != nil {
			return fErr
		}
	}
	if err := writer.Flush(); err != nil {
		if removeErr := os.Remove(envFilePath); removeErr != nil {
			return removeErr
		}
		if renameErr := os.Rename(backupPath, envFilePath); renameErr != nil {
			return renameErr
		}
		return err
	}

	// cleanup backup file
	err = os.Remove(backupPath)

	return err
}

// removeResourcesFromDeployment checks if the provided resource is a Deployment,
// and if so, removes the resources field from each container in the Deployment. This ensures we do not overwrite the
// resources field when Patch is applied with the returned unstructured resource.
func removeResourcesFromDeployment(u *unstructured.Unstructured) error {
	// Check if the resource is a Deployment. This can be expanded to other resources as well.
	if u.GetKind() != "Deployment" {
		return nil
	}
	// Navigate to the containers array in the Deployment spec
	containers, exists, err := unstructured.NestedSlice(u.Object, "spec", "template", "spec", "containers")
	if err != nil {
		return fmt.Errorf("error when trying to retrieve containers from Deployment: %w", err)
	}
	// Return if no containers exist
	if !exists {
		return nil
	}

	// Iterate over the containers to remove the resources field
	for i := range containers {
		container, ok := containers[i].(map[string]interface{})
		// If containers field is not in expected type, return.
		if !ok {
			return nil
		}
		// Check and delete the resources field. This can be expanded to any whitelisted field.
		delete(container, "resources")
		containers[i] = container
	}

	// Update the containers in the original unstructured object
	if err := unstructured.SetNestedSlice(u.Object, containers, "spec", "template", "spec", "containers"); err != nil {
		return fmt.Errorf("failed to update containers in Deployment: %w", err)
	}

	return nil
}

// GetSubscription checks if a Subscription for the operator exists in the given namespace.
// if exist, return object; otherwise, return error.
func GetSubscription(cli client.Client, namespace string, name string) (*ofapiv1alpha1.Subscription, error) {
	sub := &ofapiv1alpha1.Subscription{}
	if err := cli.Get(context.TODO(), client.ObjectKey{Namespace: namespace, Name: name}, sub); err != nil {
		// real error or 'not found' both return here
		return nil, err
	}
	return sub, nil
}

func ClusterSubscriptionExists(cli client.Client, name string) (bool, error) {
	subscriptionList := &ofapiv1alpha1.SubscriptionList{}
	if err := cli.List(context.TODO(), subscriptionList); err != nil {
		return false, err
	}

	for _, sub := range subscriptionList.Items {
		if sub.Name == name {
			return true, nil
		}
	}
	return false, nil
}

// DeleteExistingSubscription deletes given Subscription if it exists
// Do not error if the Subscription does not exist.
func DeleteExistingSubscription(cli client.Client, operatorNs string, subsName string) error {
	sub, err := GetSubscription(cli, operatorNs, subsName)
	if err != nil {
		return client.IgnoreNotFound(err)
	}

	if err := cli.Delete(context.TODO(), sub); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("error deleting subscription %s: %w", sub.Name, err)
	}

	return nil
}

// OperatorExists checks if an Operator with 'operatorPrefix' is installed.
// Return true if found it, false if not.
// if we need to check exact version of the operator installed, can append vX.Y.Z later.
func OperatorExists(cli client.Client, operatorPrefix string) (bool, error) {
	opConditionList := &ofapiv2.OperatorConditionList{}
	err := cli.List(context.TODO(), opConditionList)
	if err != nil {
		return false, err
	}
	for _, opCondition := range opConditionList.Items {
		if strings.HasPrefix(opCondition.Name, operatorPrefix) {
			return true, nil
		}
	}

	return false, nil
}

// TODO : Add function to cleanup code created as part of pre install and post install task of a component
