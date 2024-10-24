package manifest

import (
	"io/fs"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/resource"
)

type Builder struct {
	manifestLocation fs.FS
	paths            []string
}

// Location sets the root file system from which manifest paths are loaded.
func Location(fsys fs.FS) *Builder {
	return &Builder{manifestLocation: fsys}
}

// Include loads manifests from the provided paths.
func (b *Builder) Include(paths ...string) *Builder {
	b.paths = append(b.paths, paths...)
	return b
}

func (b *Builder) Create() ([]resource.Applier, error) {
	var manifests []*Manifest
	for _, path := range b.paths {
		currManifests, err := LoadManifests(b.manifestLocation, path)
		if err != nil {
			return nil, err // TODO wrap
		}

		manifests = append(manifests, currManifests...)
	}

	resources := make([]resource.Applier, 0, len(manifests))
	for _, m := range manifests {
		resources = append(resources, createApplier(m))
	}

	return resources, nil
}
