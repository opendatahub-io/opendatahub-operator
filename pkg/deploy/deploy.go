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
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/plugins"
)

const (
	DefaultManifestPath = "/opt/manifests"
)

// DownloadManifests function performs following tasks:
// 1. It takes component URI and only downloads folder specified by component.ContextDir field
// 2. It saves the manifests in the odh-manifests/component-name/ folder
func DownloadManifests(componentName string, manifestConfig components.ManifestsConfig) error {
	// Get the component repo from the given url
	// e.g  https://github.com/example/tarball/master
	resp, err := http.Get(manifestConfig.URI)
	if err != nil {
		return fmt.Errorf("error downloading manifests: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("error downloading manifests: %v HTTP status", resp.StatusCode)
	}

	// Create a new gzip reader
	gzipReader, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("error creating gzip reader: %v", err)
	}
	defer gzipReader.Close()

	// Create a new TAR reader
	tarReader := tar.NewReader(gzipReader)

	// Create manifest directory
	mode := os.ModePerm
	err = os.MkdirAll(DefaultManifestPath, mode)
	if err != nil {
		return fmt.Errorf("error creating manifests directory : %v", err)
	}

	// Extract the contents of the TAR archive to the current directory
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
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
					return fmt.Errorf("error creating directory:%v", err)
				}
				continue
			}

			if header.Typeflag == tar.TypeReg {
				file, err := os.Create(DefaultManifestPath + "/" + componentName + "/" + componentFileRelativePathFound)
				if err != nil {
					fmt.Println("Error creating file:", err)
				}
				_, err = io.Copy(file, tarReader)
				if err != nil {
					fmt.Println("Error downloading file contents:", err)
				}
				file.Close()
				continue
			}
		}
	}
	return err
}

func DeployManifestsFromPath(cli client.Client, owner metav1.Object, manifestPath string, namespace string, componentName string, componentEnabled bool) error {
	// Render the Kustomize manifests
	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	fs := filesys.MakeFsOnDisk()
	fmt.Printf("Updating manifests : %v \n", manifestPath)
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
		return fmt.Errorf("error during resmap resources: %w", err)
	}

	// Apply NamespaceTransformer Plugin
	if err := plugins.ApplyNamespacePlugin(namespace, resMap); err != nil {
		return err
	}

	// Apply LabelTransformer Plugin
	if err := plugins.ApplyAddLabelsPlugin(componentName, resMap); err != nil {
		return err
	}

	objs, err := getResources(resMap)
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

func getResources(resMap resmap.ResMap) ([]*unstructured.Unstructured, error) {
	var resources []*unstructured.Unstructured
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
		// Return nil for any errors getting the resource, since the component itself is disabled
		if err != nil {
			return nil
		}

		// Check for shared resources before deletion
		resourceLabels := found.GetLabels()
		var componentCounter []string
		if resourceLabels != nil {
			for i := range resourceLabels {
				if strings.Contains(i, "app.opendatahub.io") {
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

		if obj.GetOwnerReferences() == nil {
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
		if err = ctrl.SetControllerReference(owner, metav1.Object(obj), cli.Scheme()); err != nil {
			return err
		}
		return cli.Create(ctx, obj)
	}

	// Exception: ODHDashboardConfig should not be updated even with upgrades
	// TODO: Move this out when we have dashboard-controller
	if found.GetKind() == "OdhDashboardConfig" {
		// Do nothing, return
		return nil
	}

	// Preserve app.opendatahub.io/<component> labels of previous versions of existing objects
	foundLabels := make(map[string]string)
	for k, v := range found.GetLabels() {
		if strings.Contains(k, "app.opendatahub.io") {
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
			fmt.Printf("Failed to remove file: %v", removeErr)
		}
		if renameErr := os.Rename(backupPath, envFilePath); renameErr != nil {
			fmt.Printf("Failed to restore file from backup: %v", renameErr)
		}
		fmt.Printf("Failed to write to file: %v", err)
		return err
	}

	// cleanup backup file
	if err := os.Remove(backupPath); err != nil {
		fmt.Printf("Failed to remove backup file: %v", err)
		return err
	}

	return nil
}

// SubscriptionExists checks if a Subscription for the operator exists in the given namespace.
func SubscriptionExists(cli client.Client, namespace string, name string) (bool, error) {
	sub := &ofapiv1alpha1.Subscription{}
	if err := cli.Get(context.TODO(), client.ObjectKey{Namespace: namespace, Name: name}, sub); err != nil {
		if apierrs.IsNotFound(err) {
			return false, nil
		} else {
			return false, err
		}
	}

	return true, nil
}

// OperatorExists checks if an Operator with 'operatorPrefix' is installed.
// Return true if found it, false if not.
// TODO: if we need to check exact version of the operator installed, can append vX.Y.Z later.
func OperatorExists(cli client.Client, operatorPrefix string) (bool, error) {
	opConditionList := &ofapiv2.OperatorConditionList{}
	if err := cli.List(context.TODO(), opConditionList); err != nil {
		if !apierrs.IsNotFound(err) { // real error to run List()
			return false, err
		}
	} else {
		for _, opCondition := range opConditionList.Items {
			if strings.HasPrefix(opCondition.Name, operatorPrefix) {
				return true, nil
			}
		}
	}

	return false, nil
}

// TODO : Add function to cleanup code created as part of pre install and post install task of a component
