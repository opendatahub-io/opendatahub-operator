package kustomize

import (
	"context"
	"fmt"
	"path/filepath"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/api/resmap"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/conversion"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/resource"
)

func Create(fsys filesys.FileSystem, path string, plugins ...resmap.Transformer) *Kustomization {
	return &Kustomization{
		name:    filepath.Base(path),
		path:    path,
		fsys:    fsys,
		plugins: plugins,
	}
}

// Kustomization supports paths to kustomization files / directories containing a kustomization file
// note that it only supports to paths within the mounted files ie: /opt/manifests.
type Kustomization struct {
	name,
	path string
	fsys    filesys.FileSystem
	plugins []resmap.Transformer
}

func (k *Kustomization) Process() ([]*unstructured.Unstructured, error) {
	kustomizer := krusty.MakeKustomizer(krusty.MakeDefaultOptions())

	resMap, errRes := kustomizer.Run(k.fsys, k.path)
	if errRes != nil {
		return nil, fmt.Errorf("error during resmap resources: %w", errRes)
	}

	for _, plugin := range k.plugins {
		if err := plugin.Transform(resMap); err != nil {
			return nil, err
		}
	}

	return conversion.ResMapToUnstructured(resMap)
}

// Applier wraps an instance of Manifest and provides a way to apply it to the cluster.
type Applier struct {
	kustomization *Kustomization
}

func CreateApplier(manifest *Kustomization) *Applier {
	return &Applier{
		kustomization: manifest,
	}
}

// Apply processes owned manifest and apply it to a cluster.
func (a Applier) Apply(ctx context.Context, cli client.Client, _ map[string]any, options ...cluster.MetaOptions) error {
	objects, errProcess := a.kustomization.Process()
	if errProcess != nil {
		return errProcess
	}

	return resource.Apply(ctx, cli, objects, options...)
}
