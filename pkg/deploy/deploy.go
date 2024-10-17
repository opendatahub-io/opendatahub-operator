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
	"sigs.k8s.io/kustomize/api/resource"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	"github.com/opendatahub-io/opendatahub-operator/v2/components"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/conversion"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/plugins"
)

var (
	DefaultManifestPath = os.Getenv("DEFAULT_MANIFESTS_PATH")
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
					return fmt.Errorf("error creating file: %w", err)
				}

				defer file.Close()

				for {
					_, err := io.CopyN(file, tarReader, 1024)
					if err != nil {
						if errors.Is(err, io.EOF) {
							break
						}
						return fmt.Errorf("error downloading file contents: %w", err)
					}
				}
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

	// Create / apply / delete resources in the cluster
	for _, res := range resMap.Resources() {
		err = manageResource(ctx, cli, res, owner, namespace, componentName, componentEnabled)
		if err != nil {
			return err
		}
	}

	return nil
}

func manageResource(ctx context.Context, cli client.Client, res *resource.Resource, owner metav1.Object, applicationNamespace, componentName string, enabled bool) error {
	// Return if resource is of Kind: Namespace and Name: applicationsNamespace
	if res.GetKind() == "Namespace" && res.GetName() == applicationNamespace {
		return nil
	}

	found, err := getResource(ctx, cli, res)

	if err == nil {
		// when resource is found
		if enabled {
			// Exception to not update kserve with managed annotation
			// do not reconcile kserve resource with annotation "opendatahub.io/managed: false"
			// TODO: remove this exception when we define managed annotation across odh
			if found.GetAnnotations()[annotations.ManagedByODHOperator] == "false" && componentName == "kserve" {
				return nil
			}
			return updateResource(ctx, cli, res, found, owner)
		}
		// Delete resource if it exists or do nothing if not found
		return handleDisabledComponent(ctx, cli, found, componentName)
	}

	if !k8serr.IsNotFound(err) {
		return err
	}

	// Create resource when component enabled
	if enabled {
		return createResource(ctx, cli, res, owner)
	}
	// Skip if resource doesn't exist and component is disabled
	return nil
}

func getResource(ctx context.Context, cli client.Client, obj *resource.Resource) (*unstructured.Unstructured, error) {
	found := &unstructured.Unstructured{}
	residGvk := obj.GetGvk()
	gvk := schema.GroupVersionKind{
		Group:   residGvk.Group,
		Version: residGvk.Version,
		Kind:    residGvk.Kind,
	}
	// Setting gvk is required to do Get request
	found.SetGroupVersionKind(gvk)

	err := cli.Get(ctx, types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}, found)
	if errors.Is(err, &meta.NoKindMatchError{}) {
		// convert the error to NotFound to handle both the same way in the caller
		return nil, k8serr.NewNotFound(schema.GroupResource{Group: gvk.Group}, obj.GetName())
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

func deleteResource(ctx context.Context, cli client.Client, found *unstructured.Unstructured, componentName string) error {
	existingOwnerReferences := found.GetOwnerReferences()
	selector := labels.ODH.Component(componentName)
	resourceLabels := found.GetLabels()

	if isOwnedByODHCRD(existingOwnerReferences) || resourceLabels[selector] == "true" {
		return cli.Delete(ctx, found)
	}
	return nil
}

func createResource(ctx context.Context, cli client.Client, res *resource.Resource, owner metav1.Object) error {
	obj, err := conversion.ResourceToUnstructured(res)
	if err != nil {
		return err
	}
	if obj.GetKind() != "CustomResourceDefinition" && obj.GetKind() != "OdhDashboardConfig" {
		if err := ctrl.SetControllerReference(owner, metav1.Object(obj), cli.Scheme()); err != nil {
			return err
		}
	}
	return cli.Create(ctx, obj)
}

// Exception to skip ODHDashboardConfig CR reconcile.
func updateResource(ctx context.Context, cli client.Client, res *resource.Resource, found *unstructured.Unstructured, owner metav1.Object) error {
	if found.GetKind() == "OdhDashboardConfig" {
		return nil
	}

	// Operator reconcile allowedListfield only when resource is managed by operator(annotation is true)
	// all other cases: no annotation at all, required annotation not present, of annotation is non-true value, skip reconcile
	if managed := found.GetAnnotations()[annotations.ManagedByODHOperator]; managed != "true" {
		if err := skipUpdateOnAllowlistedFields(res); err != nil {
			return err
		}
	}

	obj, err := conversion.ResourceToUnstructured(res)
	if err != nil {
		return err
	}

	// Retain existing labels on update
	updateLabels(found, obj)

	return performPatch(ctx, cli, obj, found, owner)
}

// skipUpdateOnAllowlistedFields applies RemoverPlugin to the component's resources
// This ensures that we do not overwrite the fields when Patch is applied later to the resource.
func skipUpdateOnAllowlistedFields(res *resource.Resource) error {
	for _, rmPlugin := range plugins.AllowListedFields {
		if err := rmPlugin.TransformResource(res); err != nil {
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

// preformPatch works for update cases.
func performPatch(ctx context.Context, cli client.Client, obj, found *unstructured.Unstructured, owner metav1.Object) error {
	data, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	// force owner to be default-dsc/default-dsci
	return cli.Patch(ctx, found, client.RawPatch(types.ApplyPatchType, data), client.ForceOwnership, client.FieldOwner(owner.GetName()))
}

// TODO : Add function to cleanup code created as part of pre install and post install task of a component
