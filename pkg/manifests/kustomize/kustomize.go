package kustomize

import (
	"sigs.k8s.io/kustomize/api/krusty"
	"sigs.k8s.io/kustomize/kyaml/filesys"
)

const (
	DefaultKustomizationFileName = "kustomization.yaml"
	DefaultKustomizationFilePath = "default"
)

func NewEngine(opts ...EngineOptsFn) *Engine {
	e := Engine{
		k:  krusty.MakeKustomizer(krusty.MakeDefaultOptions()),
		fs: filesys.MakeFsOnDisk(),
		renderOpts: renderOpts{
			kustomizationFileName:    DefaultKustomizationFileName,
			kustomizationFileOverlay: DefaultKustomizationFilePath,
		},
	}

	for _, fn := range opts {
		fn(&e)
	}

	return &e
}
