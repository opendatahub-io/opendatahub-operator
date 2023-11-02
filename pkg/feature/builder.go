package feature

import (
	"github.com/pkg/errors"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
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
			AppNamespace: spec.ApplicationsNamespace,
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
			manifests, err = loadManifestsFrom(path)
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

func (fb *featureBuilder) Load() (*Feature, error) {
	feature := &Feature{
		Name:    fb.name,
		Enabled: true,
	}

	for i := range fb.builders {
		if err := fb.builders[i](feature); err != nil {
			return nil, err
		}
	}

	// UsingConfig builder wasn't called while constructing this feature.
	// Get default settings and create needed clients.
	if feature.Client == nil {
		config, err := rest.InClusterConfig()
		if errors.Is(err, rest.ErrNotInCluster) {
			// rollback to local kubeconfig - this can be helpful when running the process locally i.e. while debugging
			kubeconfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
				&clientcmd.ClientConfigLoadingRules{ExplicitPath: clientcmd.RecommendedHomeFile},
				&clientcmd.ConfigOverrides{},
			)

			config, err = kubeconfig.ClientConfig()
			if err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		}

		if err := createClients(config)(feature); err != nil {
			return nil, err
		}
	}

	if feature.Enabled {
		if err := feature.createResourceTracker(); err != nil {
			return feature, err
		}
	}

	return feature, nil
}
