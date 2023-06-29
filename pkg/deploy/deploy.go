package deploy

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"net/http"
	"os"
	"path/filepath"
	ctrl "sigs.k8s.io/controller-runtime"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/kustomize/api/filesys"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/resmap"

	"github.com/opendatahub-io/opendatahub-operator/pkg/plugins"
)

const (
	DefaultManifestPath = "/opt/odh-manifests"
)

// DownloadManifests function performs following tasks:
// 1. Given remote URI, download manifests, else extract local bundle
// 2. It saves the manifests in the odh-manifests/component-name/ folder
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

func DeployManifestsFromPath(owner metav1.Object, cli client.Client, manifestPath, namespace string, s *runtime.Scheme, componentEnabled bool) error {

	// Render the Kustomize manifests
	k := krusty.MakeKustomizer(krusty.MakeDefaultOptions())
	fs := filesys.MakeFsOnDisk()

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

	objs, err := getResources(resMap)
	if err != nil {
		return err
	}

	// Create / apply / delete resources in the cluster
	for _, obj := range objs {
		err = manageResource(owner, context.TODO(), cli, obj, s, componentEnabled)
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

func manageResource(owner metav1.Object, ctx context.Context, cli client.Client, obj *unstructured.Unstructured, s *runtime.Scheme, enabled bool) error {
	resourceName := obj.GetName()
	namespace := obj.GetNamespace()

	found := obj.DeepCopy()

	err := cli.Get(ctx, types.NamespacedName{Name: resourceName, Namespace: namespace}, found)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	// Resource exists but component is disabled
	if !enabled {
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

	// Set the owner reference for garbage collection
	if err = ctrl.SetControllerReference(owner, metav1.Object(found), s); err != nil {
		return err
	}

	// Create the resource if it doesn't exist
	if errors.IsNotFound(err) {
		return cli.Create(ctx, obj)
	}

	// Perform server-side apply
	data, err := json.Marshal(obj)
	if err != nil {
		return err
	}

	return cli.Patch(ctx, found, client.RawPatch(types.ApplyPatchType, data), client.ForceOwnership, client.FieldOwner(owner.GetName()))
}

// TODO : Add function to cleanup code created as part of preinstall and post intall task of a component
