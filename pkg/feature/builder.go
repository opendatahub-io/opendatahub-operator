package feature

import (
	"io/fs"

	"github.com/pkg/errors"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	v1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/infrastructure/v1"
)

type partialBuilder func(f *Feature) error

type featureBuilder struct {
	name     string
	builders []partialBuilder
}

func CreateFeature(name string) *featureBuilder {
	return &featureBuilder{name: name}
}

func (fb *featureBuilder) For(spec *v1.DSCInitializationSpec) *featureBuilder {
	createSpec := func(f *Feature) error {
		f.Spec = &Spec{
			AppNamespace:    spec.ApplicationsNamespace,
			ServiceMeshSpec: &spec.ServiceMesh,
			Serving:         &infrav1.ServingSpec{},
		}

		return nil
	}

	// Ensures creation of .Spec object is always invoked first
	fb.builders = append([]partialBuilder{createSpec}, fb.builders...)

	return fb
}

func (fb *featureBuilder) UsingConfig(config *rest.Config) *featureBuilder {
	fb.builders = append(fb.builders, createClients(config))

	return fb
}

func createClients(config *rest.Config) partialBuilder {
	return func(f *Feature) error {
		var err error
		f.Clientset, err = kubernetes.NewForConfig(config)
		if err != nil {
			return err
		}

		f.DynamicClient, err = dynamic.NewForConfig(config)
		if err != nil {
			return err
		}

		f.Client, err = client.New(config, client.Options{})
		if err != nil {
			return errors.WithStack(err)
		}

		if err := apiextv1.AddToScheme(f.Client.Scheme()); err != nil {
			return err
		}

		return nil
	}
}

func (fb *featureBuilder) Manifests(paths ...string) *featureBuilder {
	fb.builders = append(fb.builders, func(f *Feature) error {
		var err error
		var manifests []manifest

		for _, path := range paths {
			manifests, err = loadManifestsFrom(f.fsys, path)
			if err != nil {
				return errors.WithStack(err)
			}

			f.manifests = append(f.manifests, manifests...)
		}

		return nil
	})

	return fb
}

func (fb *featureBuilder) WithData(loader ...Action) *featureBuilder {
	fb.builders = append(fb.builders, func(f *Feature) error {
		f.loaders = append(f.loaders, loader...)

		return nil
	})

	return fb
}

func (fb *featureBuilder) PreConditions(preconditions ...Action) *featureBuilder {
	fb.builders = append(fb.builders, func(f *Feature) error {
		f.preconditions = append(f.preconditions, preconditions...)

		return nil
	})

	return fb
}

func (fb *featureBuilder) PostConditions(postconditions ...Action) *featureBuilder {
	fb.builders = append(fb.builders, func(f *Feature) error {
		f.postconditions = append(f.postconditions, postconditions...)

		return nil
	})

	return fb
}

func (fb *featureBuilder) OnDelete(cleanups ...Action) *featureBuilder {
	fb.builders = append(fb.builders, func(f *Feature) error {
		f.addCleanup(cleanups...)

		return nil
	})

	return fb
}

func (fb *featureBuilder) WithResources(resources ...Action) *featureBuilder {
	fb.builders = append(fb.builders, func(f *Feature) error {
		f.resources = resources

		return nil
	})

	return fb
}

func (fb *featureBuilder) EnabledWhen(enabled func(f *Feature) bool) *featureBuilder {
	fb.builders = append(fb.builders, func(f *Feature) error {
		f.Enabled = enabled(f)

		return nil
	})
	return fb
}

func (fb *featureBuilder) Load() (*Feature, error) {
	feature := &Feature{
		Name:    fb.name,
		Enabled: true,
		fsys:    embeddedFiles,
	}

	for i := range fb.builders {
		if err := fb.builders[i](feature); err != nil {
			return nil, err
		}
	}

	// UsingConfig builder wasn't called while constructing this feature.
	// Get default settings and create needed clients.
	if feature.Client == nil {
		restCfg, err := config.GetConfig()
		if err != nil {
			return nil, err
		}

		if err := createClients(restCfg)(feature); err != nil {
			return nil, err
		}
	}

	if feature.Enabled {
		if err := feature.createFeatureTracker(); err != nil {
			return feature, err
		}
	}

	return feature, nil
}

// ManifestSource sets the root file system (fs.FS) from which manifest paths are loaded
// If ManifestSource is not called in the builder chain, the default source will be the embeddedFiles.
func (fb *featureBuilder) ManifestSource(fsys fs.FS) *featureBuilder {
	fb.builders = append(fb.builders, func(f *Feature) error {
		f.fsys = fsys

		return nil
	})

	return fb
}
