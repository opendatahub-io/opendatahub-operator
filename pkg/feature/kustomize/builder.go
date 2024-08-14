package kustomize

import (
	"reflect"

	"sigs.k8s.io/kustomize/api/resmap"
	"sigs.k8s.io/kustomize/kyaml/filesys"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/resource"
)

type Builder struct {
	kustomizeLocation string
	fsys              filesys.FileSystem
	plugins           []resmap.Transformer
}

// Location of kustomization.yaml file to be used to determine resources to be applied.
func Location(location string) *Builder {
	return &Builder{
		kustomizeLocation: location,
	}
}

// WithPlugins allow adding transformers for the generated resources through plugins.
func (b *Builder) WithPlugins(plugins ...resmap.Transformer) *Builder {
	b.plugins = append(b.plugins, plugins...)
	return b
}

func (b *Builder) UsingFileSystem(fsys filesys.FileSystem) *Builder {
	b.fsys = fsys
	return b
}

func (b *Builder) Build() *Kustomization {
	if b.fsys == nil {
		b.fsys = filesys.MakeFsOnDisk()
	}
	return Create(b.fsys, b.kustomizeLocation, b.plugins...)
}

func (b *Builder) Create() ([]resource.Applier, error) {
	return []resource.Applier{CreateApplier(b.Build())}, nil
}

// PluginsEnricher allows to add extra plugins to the kustomize resource.
type PluginsEnricher struct {
	Plugins []resmap.Transformer
}

func (p *PluginsEnricher) AddConfig(creator resource.Builder) {
	builderValue := reflect.ValueOf(creator)
	if builderValue.Kind() == reflect.Ptr {
		builderValue = builderValue.Elem()
	}

	if builderValue.CanSet() {
		if actualBuilder, ok := builderValue.Addr().Interface().(*Builder); ok {
			actualBuilder.plugins = append(actualBuilder.plugins, p.Plugins...)
		}
	}
}
