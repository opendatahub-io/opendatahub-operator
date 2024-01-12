package feature

import (
	"github.com/pkg/errors"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	v1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/infrastructure/v1"
)

type partialBuilder func(f *Feature) error

type featureBuilder struct {
	name     string
	config   *rest.Config
	builders []partialBuilder
}

func CreateFeature(name string) *featureBuilder { //nolint:golint,revive //No need to export featureBuilder.
	return &featureBuilder{name: name}
}

func (fb *featureBuilder) For(spec *v1.DSCInitializationSpec) *featureBuilder {
	createSpec := func(f *Feature) error {
		f.Spec = &Spec{
			ServiceMeshSpec: &spec.ServiceMesh,
			Serving:         &infrav1.ServingSpec{},
			AppNamespace:    spec.ApplicationsNamespace,
		}

		return nil
	}

	// Ensures creation of .Spec object is always invoked first
	fb.builders = append([]partialBuilder{createSpec}, fb.builders...)

	return fb
}

func (fb *featureBuilder) UsingConfig(config *rest.Config) *featureBuilder {
	fb.config = config
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

		if err := apiextv1.AddToScheme(f.Client.Scheme()); err != nil { //nolint:revive,nolintlint
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

	// UsingConfig builder wasn't called while constructing this feature.
	// Get default settings and create needed clients.
	if fb.config == nil {
		if err := fb.withDefaultClient(); err != nil {
			return nil, err
		}
	}

	if err := createClients(fb.config)(feature); err != nil {
		return nil, err
	}

	for i := range fb.builders {
		if err := fb.builders[i](feature); err != nil {
			return nil, err
		}
	}

	return feature, nil
}

func (fb *featureBuilder) withDefaultClient() error {
	restCfg, err := config.GetConfig()
	if errors.Is(err, rest.ErrNotInCluster) {
		// rollback to local kubeconfig - this can be helpful when running the process locally i.e. while debugging
		kubeconfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: clientcmd.RecommendedHomeFile},
			&clientcmd.ConfigOverrides{},
		)

		restCfg, err = kubeconfig.ClientConfig()
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	fb.config = restCfg
	return nil
}
