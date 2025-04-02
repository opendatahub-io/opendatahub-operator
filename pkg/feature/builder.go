package feature

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/api/features/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/resource"
)

type partialBuilder func(f *Feature) error

type featureBuilder struct {
	featureName string
	managed     bool
	source      featurev1.Source
	owner       metav1.Object
	controller  bool
	targetNs    string

	builders []partialBuilder
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
		if setTargetNSErr := f.Set("TargetNamespace", fb.targetNs); setTargetNSErr != nil {
			return fmt.Errorf("failed to set target namespace for '%s' feature: %w", fb.featureName, setTargetNSErr)
		}

		return nil
	}

	// Ensures creation of shared data is always invoked first
	fb.builders = append([]partialBuilder{initializeContext}, fb.builders...)

	return fb
}

func (fb *featureBuilder) Source(source featurev1.Source) *featureBuilder {
	fb.source = source

	return fb
}

// TargetNamespace sets the namespace in which the feature should be applied.
// Calling it multiple times in the builder chain will have no effect, as the first value is used.
func (fb *featureBuilder) TargetNamespace(targetNs string) *featureBuilder {
	if fb.targetNs == "" {
		fb.targetNs = targetNs
	}

	return fb
}

// Manifests allow to compose manifests using different implementation of builders such as those defined in manifest and kustomize packages.
func (fb *featureBuilder) Manifests(creators ...resource.Creator) *featureBuilder {
	for _, creator := range creators {
		fb.builders = append(fb.builders, func(f *Feature) error {
			appliers, errCreate := creator.Create()
			if errCreate != nil {
				return errCreate
			}

			f.appliers = append(f.appliers, appliers...)

			return nil
		})
	}

	return fb
}

// OwnedBy is optionally used to pass down the owning object in order to set the ownerReference
// in the corresponding feature tracker.
func (fb *featureBuilder) OwnedBy(object metav1.Object) *featureBuilder {
	fb.owner = object

	return fb
}

func (fb *featureBuilder) Controller(controller bool) *featureBuilder {
	fb.controller = controller

	return fb
}

// Managed marks the feature as managed by the operator.  This effectively marks all resources which are part of this feature
// as those that should be updated on operator reconcile.
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
		f.clusterOperations = resources

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
func (fb *featureBuilder) OnDelete(cleanups ...CleanupFunc) *featureBuilder {
	fb.builders = append(fb.builders, func(f *Feature) error {
		f.addCleanup(cleanups...)

		return nil
	})

	return fb
}

// Create creates a new Feature instance and add it to corresponding FeaturesHandler.
// The actual feature creation in the cluster is not performed here.
func (fb *featureBuilder) Create() (*Feature, error) {
	alwaysEnabled := func(_ context.Context, _ client.Client, _ *Feature) (bool, error) {
		return true, nil
	}

	f := &Feature{
		Name:       fb.featureName,
		Managed:    fb.managed,
		Enabled:    alwaysEnabled,
		Log:        log.Log.WithName("features").WithValues("feature", fb.featureName),
		source:     &fb.source,
		owner:      fb.owner,
		controller: fb.controller,
	}

	for i := range fb.builders {
		if err := fb.builders[i](f); err != nil {
			return nil, err
		}
	}

	return f, nil
}
