package feature

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/hashicorp/go-multierror"
	ofapiv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/pkg/errors"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log"

	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
)

type partialBuilder func(f *Feature) error

type featureBuilder struct {
	featureName string
	source      featurev1.Source
	targetNs    string
	config      *rest.Config
	managed     bool
	fsys        fs.FS
	builders    []partialBuilder
}

// Define creates a new feature builder with the given name.
func Define(featureName string) *featureBuilder {
	fb := &featureBuilder{
		featureName: featureName,
		source: featurev1.Source{
			Type: featurev1.UnknownType,
			Name: featureName,
		},
	}

	initializeContext := func(f *Feature) error {
		if len(fb.targetNs) == 0 {
			return fmt.Errorf("target namespace for '%s' feature is not defined", fb.featureName)
		}

		f.TargetNamespace = fb.targetNs

		return f.Set("TargetNamespace", fb.targetNs)
	}

	// Ensures creation of shared data is always invoked first
	fb.builders = append([]partialBuilder{initializeContext}, fb.builders...)

	return fb
}

// TargetNamespace sets the namespace in which the feature should be applied.
func (fb *featureBuilder) TargetNamespace(targetNs string) *featureBuilder {
	fb.targetNs = targetNs

	return fb
}

func (fb *featureBuilder) Source(source featurev1.Source) *featureBuilder {
	fb.source = source

	return fb
}

// Used to enforce that Manifests() is called after ManifestsLocation() in the chain.
type requiresManifestSourceBuilder struct {
	*featureBuilder
}

// ManifestsLocation sets the root file system (fsys) from which manifest paths are loaded.
func (fb *featureBuilder) ManifestsLocation(fsys fs.FS) *requiresManifestSourceBuilder {
	fb.fsys = fsys
	return &requiresManifestSourceBuilder{featureBuilder: fb}
}

// Manifests loads manifests from the provided paths.
func (fb *requiresManifestSourceBuilder) Manifests(paths ...string) *featureBuilder {
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

	return fb.featureBuilder
}

// Managed marks the feature as managed by the operator.
//
// This effectively makes all resources which are part of this feature as reconciled to the desired state
// defined by provided manifests.
//
// NOTE: Although the actual instance of the resource in the cluster might have this configuration altered,
// we intentionally do not read the management configuration from there due to the lack of clear requirements.
// This means that management state is defined by the Feature resources provided by the operator
// and not by the actual state of the resource.
func (fb *featureBuilder) Managed() *featureBuilder {
	fb.managed = true

	return fb
}

// WithData adds data providers to the feature (implemented as Actions).
// This way you can define what data should be loaded before the feature is applied.
// This can be later used in templates and when creating resources programmatically.
func (fb *featureBuilder) WithData(dataProviders ...Action) *featureBuilder {
	fb.builders = append(fb.builders, func(f *Feature) error {
		f.dataProviders = append(f.dataProviders, dataProviders...)

		return nil
	})

	return fb
}

// EnabledWhen determines if a Feature should be loaded and applied based on specified criteria.
// The criteria are supplied as a function.
//
// Note: The function passed should consistently return true while the feature is needed.
// If the function returns false at any point, the feature's contents might be removed during the reconciliation process.
func (fb *featureBuilder) EnabledWhen(enabled EnabledFunc) *featureBuilder {
	fb.builders = append(fb.builders, func(f *Feature) error {
		f.Enabled = enabled

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

// Create creates a new Feature instance and add it to corresponding FeaturesHandler.
// The actual feature creation in the cluster is not performed here.
func (fb *featureBuilder) Create() (*Feature, error) {
	f := &Feature{
		Name:    fb.featureName,
		Managed: fb.managed,
		Enabled: func(_ context.Context, feature *Feature) (bool, error) {
			return true, nil
		},
		Log:    log.Log.WithName("features").WithValues("feature", fb.featureName),
		fsys:   fb.fsys,
		source: &fb.source,
	}

	// UsingConfig builder wasn't called while constructing this feature.
	// Get default settings and create needed clients.
	if fb.config == nil {
		if err := fb.withDefaultClient(); err != nil {
			return nil, err
		}
	}

	if err := createClient(fb.config)(f); err != nil {
		return nil, err
	}

	for i := range fb.builders {
		if err := fb.builders[i](f); err != nil {
			return nil, err
		}
	}

	return f, nil
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
