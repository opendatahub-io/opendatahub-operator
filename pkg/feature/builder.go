package feature

import (
	"io/fs"

	"github.com/hashicorp/go-multierror"
	ofapiv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/pkg/errors"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/infrastructure/v1"
)

type partialBuilder func(f *Feature) error

type featureBuilder struct {
	name            string
	config          *rest.Config
	builders        []partialBuilder
	featuresHandler *FeaturesHandler
	fsys            fs.FS
	targetNS        string
	managed         bool
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
			ServiceMeshSpec:  &featuresHandler.DSCInitializationSpec.ServiceMesh,
			Serving:          &infrav1.ServingSpec{},
			Source:           &featuresHandler.source,
			AppNamespace:     featuresHandler.DSCInitializationSpec.ApplicationsNamespace,
			AuthProviderName: "authorino",
		}

		return nil
	}

	fb := &featureBuilder{
		name:            u.name,
		featuresHandler: featuresHandler,
		targetNS:        featuresHandler.DSCInitializationSpec.ApplicationsNamespace,
	}

	// Ensures creation of .Spec object is always invoked first
	fb.builders = append([]partialBuilder{createSpec}, fb.builders...)

	return fb
}

func (fb *featureBuilder) UsingConfig(config *rest.Config) *featureBuilder {
	fb.config = config
	return fb
}

func createClient(config *rest.Config) partialBuilder {
	return func(f *Feature) error {
		var err error

		f.Client, err = client.New(config, client.Options{})
		if err != nil {
			return errors.WithStack(err)
		}

		var multiErr *multierror.Error
		s := f.Client.Scheme()
		multiErr = multierror.Append(multiErr, featurev1.AddToScheme(s), apiextv1.AddToScheme(s), ofapiv1alpha1.AddToScheme(s))

		return multiErr.ErrorOrNil()
	}
}

// Used to enforce that Manifests() is called after ManifestSource() in the chain.
type featureBuilderWithManifestSource struct {
	*featureBuilder
}

// ManifestSource sets the root file system (fsys) from which manifest paths are loaded.
func (fb *featureBuilder) ManifestSource(fsys fs.FS) *featureBuilderWithManifestSource {
	fb.fsys = fsys
	return &featureBuilderWithManifestSource{featureBuilder: fb}
}

func (fb *featureBuilderWithManifestSource) Manifests(paths ...string) *featureBuilderWithManifestSource {
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

	if err := createClient(fb.config)(feature); err != nil {
		return err
	}

	for i := range fb.builders {
		if err := fb.builders[i](feature); err != nil {
			return err
		}
	}

	feature.Spec.TargetNamespace = fb.targetNS
	feature.fsys = fb.fsys
	feature.Managed = fb.managed

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

func (fb *featureBuilder) TargetNamespace(targetNs string) *featureBuilder {
	fb.targetNS = targetNs

	return fb
}

func (fb *featureBuilder) Managed() *featureBuilder {
	fb.managed = true

	return fb
}
