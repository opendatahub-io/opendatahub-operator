package feature

import (
	"io/fs"

	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/infrastructure/v1"
)

type partialBuilder func(f *Feature) error

type featureBuilder struct {
	name            string
	config          *rest.Config
	builders        []partialBuilder
	featuresHandler *FeaturesHandler
	fsys            fs.FS
	targetNS        string
}

func CreateFeature(name string) *usingFeaturesHandler { //nolint:golint,revive //No need to export featureBuilder.
	return &usingFeaturesHandler{
		name: name,
	}
}

type usingFeaturesHandler struct {
	name string
}

func (u *usingFeaturesHandler) For(featuresHandler *FeaturesHandler) *featureBuilder {
	createSpec := func(f *Feature) error {
		f.Spec = &Spec{
			ServiceMeshSpec: &featuresHandler.DSCInitializationSpec.ServiceMesh,
			Serving:         &infrav1.ServingSpec{},
			Source:          &featuresHandler.source,
			AppNamespace:    featuresHandler.DSCInitializationSpec.ApplicationsNamespace,
		}

		return nil
	}

	fb := &featureBuilder{
		name:            u.name,
		featuresHandler: featuresHandler,
		fsys:            embeddedFiles,
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

		var multiErr *multierror.Error
		s := f.Client.Scheme()
		multiErr = multierror.Append(multiErr, featurev1.AddToScheme(s), apiextv1.AddToScheme(s))

		return multiErr.ErrorOrNil()
	}
}

func (fb *featureBuilder) Manifests(paths ...string) *featureBuilder {
	fb.builders = append(fb.builders, func(f *Feature) error {
		var err error
		var manifests []Manifest

		for _, path := range paths {
			manifests, err = loadManifestsFrom(fb.fsys, path)
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

func (fb *featureBuilder) Load() error {
	feature := newFeature(fb.name)

	// UsingConfig builder wasn't called while constructing this feature.
	// Get default settings and create needed clients.
	if fb.config == nil {
		if err := fb.withDefaultClient(); err != nil {
			return err
		}
	}

	if err := createClients(fb.config)(feature); err != nil {
		return err
	}

	for i := range fb.builders {
		if err := fb.builders[i](feature); err != nil {
			return err
		}
	}

	// default TargetNamespace to AppNamespace if not explicitly set
	if feature.Spec.TargetNamespace == "" {
		feature.Spec.TargetNamespace = feature.Spec.AppNamespace
	}

	fb.featuresHandler.features = append(fb.featuresHandler.features, feature)

	return nil
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

// ManifestSource sets the root file system (fsys) from which manifest paths are loaded
// If ManifestSource is not called in the builder chain, the default source will be the embeddedFiles.
func (fb *featureBuilder) ManifestSource(fsys fs.FS) *featureBuilder {
	fb.fsys = fsys
	return fb
}

func (fb *featureBuilder) TargetNamespace(targetNs string) *featureBuilder {
	fb.targetNS = targetNs

	setTargetNamespace := func(f *Feature) error {
		if f.Spec == nil {
			return errors.New("Spec has not been initialized")
		}
		f.Spec.TargetNamespace = fb.targetNS
		return nil
	}

	fb.builders = append(fb.builders, setTargetNamespace)

	return fb
}
