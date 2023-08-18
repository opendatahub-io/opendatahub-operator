package deploy

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
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

	"fmt"
	"github.com/go-logr/logr"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/plugins"
	operators "github.com/operator-framework/api/pkg/operators/v1alpha1"
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
			ctrl.Log.Error(err, "error downloading manifest: "+err.Error())
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			ctrl.Log.Error(err, "error downloading manifest", "HTTP status", resp.StatusCode)
			return err
		}
		reader = resp.Body

		// Create a new gzip reader
		gzipReader, err := gzip.NewReader(reader)
		if err != nil {
			ctrl.Log.Error(err, "error creating gzip reader: "+err.Error())
			return err
		}
		defer gzipReader.Close()

		// Create a new TAR reader
		tarReader := tar.NewReader(gzipReader)

		// Create manifest directory
		mode := os.ModePerm
		err = os.MkdirAll(DefaultManifestPath, mode)
		if err != nil {
			ctrl.Log.Error(err, "error creating manifests directory "+err.Error())
			return err
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

func DeployManifestsFromPath(
	owner metav1.Object,
	cli client.Client,
	componentName string,
	manifestPath string,
	namespace string,
	s *runtime.Scheme,
	componentEnabled bool,
	logger logr.Logger,
) error {

	// Render the Kustomize manifests
	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	fs := filesys.MakeFsOnDisk()
	logger.Info("Reconciling " + componentName + " from manifests: " + manifestPath)

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
		logger.Error(err, "Error during resmap resources:", "path", manifestPath)
		return err
	}

	// Apply NamespaceTransformer Plugin
	if err := plugins.ApplyNamespacePlugin(namespace, resMap); err != nil {
		logger.Error(err, "Error apply namespace plugin", "error", err.Error())
		return err
	}

	// Apply LabelTransformer Plugin
	if err := plugins.ApplyAddLabelsPlugin(componentName, resMap); err != nil {
		logger.Error(err, "Error apply label plugin", "error", err.Error())
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

func manageResource(
	owner metav1.Object,
	ctx context.Context,
	cli client.Client,
	obj *unstructured.Unstructured,
	s *runtime.Scheme,
	enabled bool,
	applicationNamespace string,
	componentName string,
) error {
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
	for key, _ := range envMap {
		relatedImageValue := os.Getenv(imageParamsMap[key])
		if relatedImageValue != "" {
			envMap[key] = relatedImageValue
		}
	}

	// Move the existing file to a backup file
	os.Rename(envFilePath, backupPath)

	// Now, write the map back to the file
	file, err = os.Create(envFilePath)
	if err != nil {
		// If create fails, restore the backup file
		os.Rename(backupPath, envFilePath)
		return err
	}
	defer file.Close()

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

	if err := os.Remove(backupPath); err != nil {
		fmt.Printf("Failed to remove backup file: %v", err)
		return err
	}
	return nil
}

// Checks if a Subscription for the an operator exists in the given namespace
func SubscriptionExists(cli client.Client, namespace string, name string) (bool, error) {
	sub := &operators.Subscription{}
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

// TODO : Add function to cleanup code created as part of pre install and post intall task of a component
