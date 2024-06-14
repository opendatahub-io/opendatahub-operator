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

// CreateFeature creates a new feature builder with the given name.
func CreateFeature(name string) *usingFeaturesHandler { //nolint:golint,revive //No need to export featureBuilder.
	return &usingFeaturesHandler{
		name: name,
	}
}

type usingFeaturesHandler struct {
	name string
}

// For sets the associated FeaturesHandler for the feature which will serve as entry point managing all the related features.
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

// Used to enforce that Manifests() is called after ManifestsLocation() in the chain.
type featureBuilderWithManifestSource struct {
	*featureBuilder
}

// ManifestsLocation sets the root file system (fsys) from which manifest paths are loaded.
func (fb *featureBuilder) ManifestsLocation(fsys fs.FS) *featureBuilderWithManifestSource {
	fb.fsys = fsys
	return &featureBuilderWithManifestSource{featureBuilder: fb}
}

// Manifests loads manifests from the provided paths.
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

// TargetNamespace sets the namespace in which the feature should be applied.
// If not set, the feature will be applied in the application namespace (where this operator lives).
func (fb *featureBuilder) TargetNamespace(targetNs string) *featureBuilder {
	fb.targetNS = targetNs

	return fb
}

// Managed marks the feature as managed by the operator.  This effectively marks all resources which are part of this feature
// as those that should be updated on operator reconcile.
func (fb *featureBuilder) Managed() *featureBuilder {
	fb.managed = true

	return fb
}

// WithData adds data loaders to the feature. This way you can define what data should be loaded before the feature is applied.
// This can be later used in templates and when creating resources programmatically.
func (fb *featureBuilder) WithData(loader ...Action) *featureBuilder {
	fb.builders = append(fb.builders, func(f *Feature) error {
		f.loaders = append(f.loaders, loader...)

		return nil
	})

	return fb
}

// WithResources allows to define programmatically which resources should be created when applying defined Feature.
func (fb *featureBuilder) WithResources(resources ...Action) *featureBuilder {
	fb.builders = append(fb.builders, func(f *Feature) error {
		f.resources = resources

		return nil
	})

	return fb
}

// PreConditions adds preconditions to the feature. Preconditions are actions that are executed before the feature is applied.
// They can be used to check if the feature can be applied by inspecting the cluster state or by executing some arbitrary checks.
// If any of the precondition fails, the feature will not be applied.
func (fb *featureBuilder) PreConditions(preconditions ...Action) *featureBuilder {
	fb.builders = append(fb.builders, func(f *Feature) error {
		f.preconditions = append(f.preconditions, preconditions...)

		return nil
	})

	return fb
}

// PostConditions adds postconditions to the feature. Postconditions are actions that are executed after the feature is applied.
func (fb *featureBuilder) PostConditions(postconditions ...Action) *featureBuilder {
	fb.builders = append(fb.builders, func(f *Feature) error {
		f.postconditions = append(f.postconditions, postconditions...)

		return nil
	})

	return fb
}

// OnDelete allow to add cleanup hooks that are executed when the feature is going to be deleted.
// By default, all resources created by the feature are deleted when the feature is deleted, so there is no need to
// explicitly add cleanup hooks for them.
//
// This is useful when you need to perform some additional cleanup actions such as removing effects of a patch operation.
func (fb *featureBuilder) OnDelete(cleanups ...Action) *featureBuilder {
	fb.builders = append(fb.builders, func(f *Feature) error {
		f.addCleanup(cleanups...)

		return nil
	})

	return fb
}

// Load creates a new Feature instance and add it to corresponding FeaturesHandler.
// The actual feature creation in the cluster is not performed here.
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

// UsingConfig allows to pass a custom rest.Config to the feature. Useful for testing.
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
