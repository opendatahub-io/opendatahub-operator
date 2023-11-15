package feature

import (
	"github.com/hashicorp/go-multierror"

	v1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
)

type FeaturesInitializer struct {
	*v1.DSCInitializationSpec
	definedFeatures DefinedFeatures
	Features        []*Feature
}

type DefinedFeatures func(s *FeaturesInitializer) error

func NewFeaturesInitializer(spec *v1.DSCInitializationSpec, def DefinedFeatures) *FeaturesInitializer {
	return &FeaturesInitializer{
		DSCInitializationSpec: spec,
		definedFeatures:       def,
	}
}

// Prepare performs validation of the spec and ensures all resources,
// such as Features and their templates, are processed and initialized
// before proceeding with the actual cluster set-up.
func (s *FeaturesInitializer) Prepare() error {
	log.Info("Initializing features")

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
		log.Info("cleanup", "name", s.Features[i].Name)
		cleanupErrors = multierror.Append(cleanupErrors, s.Features[i].Cleanup())
	}

	return cleanupErrors.ErrorOrNil()
}
