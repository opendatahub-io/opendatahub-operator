package feature

import (
	"github.com/hashicorp/go-multierror"

	v1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components"
)

type FeaturesInitializer struct {
	*v1.DSCInitializationSpec
	Source          featurev1.Source
	Features        []*Feature
	definedFeatures DefinedFeatures
}

type DefinedFeatures func(initializer *FeaturesInitializer) error

func ClusterFeaturesInitializer(dsci *v1.DSCInitialization, def DefinedFeatures) *FeaturesInitializer {
	return &FeaturesInitializer{
		DSCInitializationSpec: &dsci.Spec,
		Source:                featurev1.Source{Type: featurev1.DSCIType, Name: dsci.Name},
		definedFeatures:       def,
	}
}

func ComponentFeaturesInitializer(component components.ComponentInterface, spec *v1.DSCInitializationSpec, def DefinedFeatures) *FeaturesInitializer {
	return &FeaturesInitializer{
		DSCInitializationSpec: spec,
		Source:                featurev1.Source{Type: featurev1.ComponentType, Name: component.GetComponentName()},
		definedFeatures:       def,
	}
}

// Prepare performs validation of the spec and ensures all resources,
// such as Features and their templates, are processed and initialized
// before proceeding with the actual cluster set-up.
func (s *FeaturesInitializer) Prepare() error {
	return s.definedFeatures(s)
}

func (s *FeaturesInitializer) Apply() error {
	var applyErrors *multierror.Error

	for _, f := range s.Features {
		applyErrors = multierror.Append(applyErrors, f.Apply())
	}

	return applyErrors.ErrorOrNil()
}

// Delete executes registered clean-up tasks in the opposite order they were initiated (following a stack structure).
// For instance, this allows for the undoing patches before its deletion.
// This approach assumes that Features are either instantiated in the correct sequence
// or are self-contained.
func (s *FeaturesInitializer) Delete() error {
	var cleanupErrors *multierror.Error
	for i := len(s.Features) - 1; i >= 0; i-- {
		cleanupErrors = multierror.Append(cleanupErrors, s.Features[i].Cleanup())
	}

	return cleanupErrors.ErrorOrNil()
}
