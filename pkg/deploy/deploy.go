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
	"golang.org/x/exp/maps"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/kustomize/api/filesys"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/resmap"

	ofapiv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/plugins"

	ofapiv2 "github.com/operator-framework/api/pkg/operators/v2"
)

const (
	DefaultManifestPath = "/opt/manifests"
)

// DownloadManifests function performs following tasks:
// 1. Given remote URI, download manifests, else extract local bundle
// 2. It saves the manifests in the /opt/manifests/component-name/ folder
func DownloadManifests(uri string) error {
	// Get the component repo from the given url
	// e.g  https://github.com/example/tarball/master\
	var reader io.Reader
	if uri != "" {
		resp, err := http.Get(uri)
		if err != nil {
			return fmt.Errorf("error downloading manifests: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("error downloading manifests: %v HTTP status", resp.StatusCode)
		}
		reader = resp.Body

		// Create a new gzip reader
		gzipReader, err := gzip.NewReader(reader)
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

		for {
			header, err := tarReader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			manifestsPath := strings.Split(header.Name, "/")

			// Determine the file or directory path to extract to
			target := filepath.Join(DefaultManifestPath, strings.Join(manifestsPath[1:], "/"))

			if header.Typeflag == tar.TypeDir {
				// Create directories
				err = os.MkdirAll(target, os.ModePerm)
				if err != nil {
					return err
				}
			} else if header.Typeflag == tar.TypeReg {
				// Extract regular files
				outputFile, err := os.Create(target)
				if err != nil {
					return err
				}
				defer outputFile.Close()

				_, err = io.Copy(outputFile, tarReader)
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func DeployManifestsFromPath(owner metav1.Object, cli client.Client, componentName, manifestPath, namespace string, s *runtime.Scheme, componentEnabled bool) error {

	// Render the Kustomize manifests
	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	fs := filesys.MakeFsOnDisk()
	fmt.Printf("Updating manifests : %v \n", manifestPath)
	// Create resmap
	// Use kustomization file under manifestPath or use `default` overlay
	var resMap resmap.ResMap
	_, err := os.Stat(manifestPath + "/kustomization.yaml")
	if err != nil {
		if os.IsNotExist(err) {
			resMap, err = k.Run(fs, manifestPath+"/default")
		}
	} else {
		resMap, err = k.Run(fs, manifestPath)
	}

	if err != nil {
		return fmt.Errorf("error during resmap resources: %v", err)
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
		err = manageResource(owner, context.TODO(), cli, obj, s, componentEnabled, namespace, componentName)
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

func manageResource(owner metav1.Object, ctx context.Context, cli client.Client, obj *unstructured.Unstructured, s *runtime.Scheme, enabled bool, applicationNamespace, componentName string) error {
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
			for key, _ := range resourceLabels {
				if strings.Contains(key, "app.opendatahub.io") {
					compFound := strings.Split(key, "/")[1]
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
	if errors.IsNotFound(err) {
		// Set the owner reference for garbage collection
		if err = ctrl.SetControllerReference(owner, metav1.Object(obj), s); err != nil {
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
- image values set in manifests params.env if manifestsURI is not set
*/
func ApplyImageParams(componentPath string, imageParamsMap map[string]string) error {
	envFilePath := componentPath + "/params.env"
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
	for key, _ := range envMap {
		relatedImageValue := os.Getenv(imageParamsMap[key])
		if relatedImageValue != "" {
			envMap[key] = relatedImageValue
		}
	}

	// Move the existing file to a backup file and create empty file
	os.Rename(envFilePath, backupPath)
	file, err = os.Create(envFilePath)
	if err != nil {
		// If create fails, restore the backup file
		os.Rename(backupPath, envFilePath)
		return err
	}
	defer file.Close()

	// Now, write the map back to the file
	writer := bufio.NewWriter(file)
	for key, value := range envMap {
		fmt.Fprintf(writer, "%s=%s\n", key, value)
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

// Checks if a Subscription for the an operator exists in the given namespace
func SubscriptionExists(cli client.Client, namespace string, name string) (bool, error) {
	sub := &ofapiv1alpha1.Subscription{}
	err := cli.Get(context.TODO(), client.ObjectKey{Namespace: namespace, Name: name}, sub)
	if err != nil {
		if apierrs.IsNotFound(err) {
			return false, nil
		} else {
			return false, err
		}
	}
	return true, nil
}

// Checks if an Operator with operatorprefix is installed in the given namespace list
// TODO: if we need to check exact verison of the operator installed, can append vX.Y.Z later
// Return true if found it, false if not.
func OperatorExists(cli client.Client, operatorprefix string) (bool, error) {
	opConditionList := &ofapiv2.OperatorConditionList{}
	err := cli.List(context.TODO(), opConditionList)
	if err != nil {
		if !apierrs.IsNotFound(err) { // real error to run List()
			return false, err
		}
	} else {
		for _, opCondition := range opConditionList.Items {
			if strings.HasPrefix(string(opCondition.Name), operatorprefix) {
				return true, nil
			}
		}
	}
	return false, nil
}

// TODO : Add function to cleanup code created as part of pre install and post intall task of a component
