package kustomize

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/plugins"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

type Engine struct {
	k          *krusty.Kustomizer
	fs         filesys.FileSystem
	renderOpts renderOpts
}

func (e *Engine) Render(path string, opts ...RenderOptsFn) ([]unstructured.Unstructured, error) {
	// poor man clone
	ro := e.renderOpts
	ro.labels = maps.Clone(e.renderOpts.labels)
	ro.annotations = maps.Clone(e.renderOpts.annotations)
	ro.plugins = slices.Clone(e.renderOpts.plugins)

	for _, fn := range opts {
		fn(&ro)
	}

	if !e.fs.Exists(filepath.Join(path, ro.kustomizationFileName)) {
		path = filepath.Join(path, ro.kustomizationFileOverlay)
	}

	resMap, err := e.k.Run(e.fs, path)
	if err != nil {
		return nil, err
	}

	if ro.ns != "" {
		plugin := plugins.CreateNamespaceApplierPlugin(ro.ns)
		if err := plugin.Transform(resMap); err != nil {
			return nil, fmt.Errorf("failed applying namespace plugin when preparing Kustomize resources. %w", err)
		}
	}

	if len(ro.labels) != 0 {
		plugin := plugins.CreateSetLabelsPlugin(ro.labels)
		if err := plugin.Transform(resMap); err != nil {
			return nil, fmt.Errorf("failed applying labels plugin when preparing Kustomize resources. %w", err)
		}
	}

	if len(ro.annotations) != 0 {
		plugin := plugins.CreateAddAnnotationsPlugin(ro.annotations)
		if err := plugin.Transform(resMap); err != nil {
			return nil, fmt.Errorf("failed applying annotations plugin when preparing Kustomize resources. %w", err)
		}
	}

	for i := range ro.plugins {
		if err := ro.plugins[i].Transform(resMap); err != nil {
			return nil, fmt.Errorf("failed applying %v plugin when preparing Kustomize resources. %w", ro.plugins[i], err)
		}
	}

	renderedRes := resMap.Resources()
	resp := make([]unstructured.Unstructured, len(renderedRes))

	for i := range renderedRes {
		m, err := renderedRes[i].Map()
		if err != nil {
			return nil, fmt.Errorf("failed to transform Resources to Unstructured. %w", err)
		}

		u, err := resources.ToUnstructured(&m)
		if err != nil {
			return nil, fmt.Errorf("failed to transform Resources to Unstructured. %w", err)
		}

		resp[i] = *u
	}

	return resp, nil
}
