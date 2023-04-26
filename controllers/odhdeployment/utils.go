package odhdeployment

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/kustomize/api/resmap"

	odhdeploymentv1 "github.com/opendatahub-io/opendatahub-operator/api/v1"
	"io"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"net/http"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"strings"
)

// downloadManifests function performs following tasks:
// 1. It takes component URI and only downloads folder specified by component.ManifestFolder field
// 2. It saves the manifests in the odh-manifests/component-name/ folder
func downloadManifests(component odhdeploymentv1.Component) error {
	// Get the component repo from the given url
	// e.g  https://github.com/example/tarball/master
	resp, err := http.Get(component.URL)
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
	err = os.MkdirAll("/tmp/odh-manifests", mode)
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
		componentManifestPath := componentFiles[0] + "/" + component.ManifestFolder

		if strings.Contains(componentFileName, componentManifestPath) {
			// Get manifest path relative to repo
			// e.g. of repo/a/b/manifests/base --> base/
			componentFoldersList := strings.Split(componentFileName, "/")
			componentFileRelativePathFound := strings.Join(componentFoldersList[len(strings.Split(componentManifestPath, "/")):], "/")

			if header.Typeflag == tar.TypeDir {

				err = os.MkdirAll(localManifestFolder+component.Name+"/"+componentFileRelativePathFound, mode)
				if err != nil {
					return fmt.Errorf("error creating directory:%v", err)
				}
				continue
			}

			if header.Typeflag == tar.TypeReg {
				file, err := os.Create(localManifestFolder + component.Name + "/" + componentFileRelativePathFound)
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

func (r *OdhDeploymentReconciler) createOrUpdate(ctx context.Context, instance *odhdeploymentv1.OdhDeployment, obj *unstructured.Unstructured) error {
	fmt.Printf("Creating resource :%v", obj.UnstructuredContent())
	found := obj.DeepCopy()
	err := r.Get(ctx, types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}, found)
	if err != nil && errors.IsNotFound(err) {
		// Set the owner reference for garbage collection
		if err := controllerutil.SetControllerReference(instance, obj, r.Scheme); err != nil {
			return err
		}
		// Create the resource if it doesn't exist
		return r.Create(ctx, obj)
	} else if err != nil {
		return err
	}

	// Update the resource if it exists
	obj.SetResourceVersion(found.GetResourceVersion())
	return r.Update(ctx, obj)
}
