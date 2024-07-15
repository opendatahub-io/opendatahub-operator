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

// Package deploy provides utility functions used by each component to deploy manifests to the cluster.
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

	"golang.org/x/exp/maps"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/resmap"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/conversion"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/plugins"
)

const (
	DefaultManifestPath = "/opt/manifests"
)

// DownloadManifests function performs following tasks:
// 1. It takes component URI and only downloads folder specified by component.ContextDir field
// 2. It saves the manifests in the odh-manifests/component-name/ folder.
func DownloadManifests(ctx context.Context, componentName string, manifestConfig components.ManifestsConfig) error {
	// Get the component repo from the given url
	// e.g.  https://github.com/example/tarball/master
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestConfig.URI, nil)
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

func DeployManifestsFromPath(
	ctx context.Context,
	cli client.Client,
	owner metav1.Object,
	manifestPath string,
	namespace string,
	componentName string,
	componentEnabled bool,
) error {
	// Render the Kustomize manifests
	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	fs := filesys.MakeFsOnDisk()
	// Create resmap
	// Use kustomization file under manifestPath or use `default` overlay
	var resMap resmap.ResMap
	_, err := os.Stat(filepath.Join(manifestPath, "kustomization.yaml"))
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		manifestPath = filepath.Join(manifestPath, "default")
	}

	resMap, err = k.Run(fs, manifestPath)
	if err != nil {
		return err
	}

	nsPlugin := plugins.CreateNamespaceApplierPlugin(namespace)
	if err := nsPlugin.Transform(resMap); err != nil {
		return fmt.Errorf("failed applying namespace plugin when preparing Kustomize resources. %w", err)
	}

	labelsPlugin := plugins.CreateAddLabelsPlugin(componentName)
	if err := labelsPlugin.Transform(resMap); err != nil {
		return fmt.Errorf("failed applying labels plugin when preparing Kustomize resources. %w", err)
	}

	objs, err := conversion.ResMapToUnstructured(resMap)
	if err != nil {
		return err
	}
	// Create / apply / delete resources in the cluster
	for _, obj := range objs {
		err = manageResource(ctx, cli, obj, owner, namespace, componentName, componentEnabled)
		if err != nil {
			return err
		}
	}

	return nil
}

func manageResource(ctx context.Context, cli client.Client, obj *unstructured.Unstructured, owner metav1.Object, applicationNamespace, componentName string, enabled bool) error {
	// Skip if resource is of Kind: Namespace and Name: applicationNamespace
	if obj.GetKind() == "Namespace" && obj.GetName() == applicationNamespace {
		return nil
	}

	found, err := getResource(ctx, cli, obj)

	if err != nil {
		if !k8serr.IsNotFound(err) {
			return err
		}
		// Create resource if it doesn't exist and component enabled
		if enabled {
			return createResource(ctx, cli, obj, owner)
		}
		// Skip if resource doesn't exist and component is disabled
		return nil
	}

	// when resource is found
	if enabled {
		// Exception to not update kserve with managed annotation
		// do not reconcile kserve resource with annotation "opendatahub.io/managed: false"
		// TODO: remove this exception when we define managed annotation across odh
		if found.GetAnnotations()[annotations.ManagedByODHOperator] == "false" && componentName == "kserve" {
			return nil
		}
		return updateResource(ctx, cli, obj, found, owner, componentName)
	}
	// Delete resource if it exists and component is disabled
	return handleDisabledComponent(ctx, cli, found, componentName)
}

/*
overwrite values in components' manifests params.env file
This is useful for air gapped cluster
priority of image values (from high to low):
- image values set in manifests params.env if manifestsURI is set
- RELATED_IMAGE_* values from CSV (if it is set)
- image values set in manifests params.env if manifestsURI is not set.
parameter isUpdateNamespace is used to set if should update namespace  with DSCI CR's applicationnamespace.
extraParamsMaps is used to set extra parameters which are not carried from ENV variable. this can be passed per component.
*/
func ApplyParams(componentPath string, imageParamsMap map[string]string, isUpdateNamespace bool, extraParamsMaps ...map[string]string) error {
	paramsFile := filepath.Join(componentPath, "params.env")
	// Require params.env at the root folder
	paramsEnv, err := os.Open(paramsFile)
	if err != nil {
		if os.IsNotExist(err) {
			// params.env doesn't exist, do not apply any changes
			return nil
		}
		return err
	}

	defer paramsEnv.Close()

	paramsEnvMap := make(map[string]string)
	scanner := bufio.NewScanner(paramsEnv)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			paramsEnvMap[parts[0]] = parts[1]
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	// 1. Update images with env variables
	// e.g "odh-kuberay-operator-controller-image": "RELATED_IMAGE_ODH_KUBERAY_OPERATOR_CONTROLLER_IMAGE",
	for i := range paramsEnvMap {
		relatedImageValue := os.Getenv(imageParamsMap[i])
		if relatedImageValue != "" {
			paramsEnvMap[i] = relatedImageValue
		}
	}

	// 2. Update namespace variable with applicationNamepsace
	if isUpdateNamespace {
		paramsEnvMap["namespace"] = imageParamsMap["namespace"]
	}

	// 3. Update other fileds with extraParamsMap which are not carried from component
	for _, extraParamsMap := range extraParamsMaps {
		for eKey, eValue := range extraParamsMap {
			paramsEnvMap[eKey] = eValue
		}
	}

	// Move the existing file to a backup file and create empty file
	paramsBackupFile := paramsFile + ".bak"
	if err := os.Rename(paramsFile, paramsBackupFile); err != nil {
		return err
	}

	file, err := os.Create(paramsFile)
	if err != nil {
		// If create fails, try to restore the backup file
		_ = os.Rename(paramsBackupFile, paramsFile)
		return err
	}
	defer file.Close()

	// Now, write the new map back to params.env
	writer := bufio.NewWriter(file)
	for key, value := range paramsEnvMap {
		if _, fErr := fmt.Fprintf(writer, "%s=%s\n", key, value); fErr != nil {
			return fErr
		}
	}
	if err := writer.Flush(); err != nil {
		if removeErr := os.Remove(paramsFile); removeErr != nil {
			fmt.Printf("Failed to remove file: %v", removeErr)
		}
		if renameErr := os.Rename(paramsBackupFile, paramsFile); renameErr != nil {
			fmt.Printf("Failed to restore file from backup: %v", renameErr)
		}
		fmt.Printf("Failed to write to file: %v", err)
		return err
	}

	// cleanup backup file params.env.bak
	if err := os.Remove(paramsBackupFile); err != nil {
		fmt.Printf("Failed to remove backup file: %v", err)
		return err
	}

	return nil
}

// removeResourcesFromDeployment checks if the provided resource is a Deployment,
// and if so, removes the resources field from each container in the Deployment. This ensures we do not overwrite the
// resources field when Patch is applied with the returned unstructured resource.
// TODO: remove this function once RHOAIENG-7929 is finished.
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

func getResource(ctx context.Context, cli client.Client, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	found := &unstructured.Unstructured{}
	// Setting gvk is required to do Get request
	found.SetGroupVersionKind(obj.GroupVersionKind())
	err := cli.Get(ctx, types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}, found)
	if errors.Is(err, &meta.NoKindMatchError{}) {
		// convert the error to NotFound to handle both the same way in the caller
		return nil, k8serr.NewNotFound(schema.GroupResource{Group: obj.GroupVersionKind().Group}, obj.GetName())
	}
	if err != nil {
		return nil, err
	}
	return found, nil
}

func handleDisabledComponent(ctx context.Context, cli client.Client, found *unstructured.Unstructured, componentName string) error {
	resourceLabels := found.GetLabels()
	componentCounter := getComponentCounter(resourceLabels)

	if isSharedResource(componentCounter, componentName) || found.GetKind() == "CustomResourceDefinition" {
		return nil
	}

	return deleteResource(ctx, cli, found, componentName)
}

func getComponentCounter(foundLabels map[string]string) []string {
	var componentCounter []string
	for label := range foundLabels {
		if strings.Contains(label, labels.ODHAppPrefix) {
			compFound := strings.Split(label, "/")[1]
			componentCounter = append(componentCounter, compFound)
		}
	}
	return componentCounter
}

func isSharedResource(componentCounter []string, componentName string) bool {
	return len(componentCounter) > 1 || (len(componentCounter) == 1 && componentCounter[0] != componentName)
}

func deleteResource(ctx context.Context, cli client.Client, found *unstructured.Unstructured, componentName string) error {
	existingOwnerReferences := found.GetOwnerReferences()
	selector := labels.ODH.Component(componentName)
	resourceLabels := found.GetLabels()

	if isOwnedByODHCRD(existingOwnerReferences) || resourceLabels[selector] == "true" {
		return cli.Delete(ctx, found)
	}
	return nil
}

func isOwnedByODHCRD(ownerReferences []metav1.OwnerReference) bool {
	for _, owner := range ownerReferences {
		if owner.Kind == "DataScienceCluster" || owner.Kind == "DSCInitialization" {
			return true
		}
	}
	return false
}

func createResource(ctx context.Context, cli client.Client, obj *unstructured.Unstructured, owner metav1.Object) error {
	if obj.GetKind() != "CustomResourceDefinition" && obj.GetKind() != "OdhDashboardConfig" {
		if err := ctrl.SetControllerReference(owner, metav1.Object(obj), cli.Scheme()); err != nil {
			return err
		}
	}
	return cli.Create(ctx, obj)
}

// TODO: remove this once RHOAIENG-7929 is finished.
func skipUpdateOnWhitelistedFields(obj *unstructured.Unstructured, componentName string) error {
	if componentName == "kserve" || componentName == "model-mesh" {
		if err := removeResourcesFromDeployment(obj); err != nil {
			return err
		}
	}
	return nil
}

func updateLabels(found, obj *unstructured.Unstructured) {
	foundLabels := make(map[string]string)
	for k, v := range found.GetLabels() {
		if strings.Contains(k, labels.ODHAppPrefix) {
			foundLabels[k] = v
		}
	}
	newLabels := obj.GetLabels()
	maps.Copy(foundLabels, newLabels)
	obj.SetLabels(foundLabels)
}

func performPatch(ctx context.Context, cli client.Client, obj, found *unstructured.Unstructured, owner metav1.Object) error {
	data, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	return cli.Patch(ctx, found, client.RawPatch(types.ApplyPatchType, data), client.ForceOwnership, client.FieldOwner(owner.GetName()))
}

func updateResource(ctx context.Context, cli client.Client, obj, found *unstructured.Unstructured, owner metav1.Object, componentName string) error {
	// Skip ODHDashboardConfig Update
	if found.GetKind() == "OdhDashboardConfig" {
		return nil
	}
	// skip updating whitelisted fields from component
	if err := skipUpdateOnWhitelistedFields(obj, componentName); err != nil {
		return err
	}

	// Retain existing labels on update
	updateLabels(found, obj)

	return performPatch(ctx, cli, obj, found, owner)
}

// TODO : Add function to cleanup code created as part of pre install and post install task of a component
