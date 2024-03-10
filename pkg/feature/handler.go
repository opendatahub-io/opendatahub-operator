package feature

import (
	"fmt"

	"github.com/hashicorp/go-multierror"

	v1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
)

// FeaturesHandler coordinates feature creations and removal from within controllers.
type FeaturesHandler struct {
	*v1.DSCInitializationSpec
	source           featurev1.Source
	features         []*Feature
	featuresProvider FeaturesProvider
}

// FeaturesProvider is a function which allow to define list of features
// and couple them with the given initializer.
type FeaturesProvider func(handler *FeaturesHandler) error

func ClusterFeaturesHandler(dsci *v1.DSCInitialization, def FeaturesProvider) *FeaturesHandler {
	return &FeaturesHandler{
		DSCInitializationSpec: &dsci.Spec,
		source:                featurev1.Source{Type: featurev1.DSCIType, Name: dsci.Name},
		featuresProvider:      def,
	}
}

func ComponentFeaturesHandler(componentName string, spec *v1.DSCInitializationSpec, def FeaturesProvider) *FeaturesHandler {
	return &FeaturesHandler{
		DSCInitializationSpec: spec,
		source:                featurev1.Source{Type: featurev1.ComponentType, Name: componentName},
		featuresProvider:      def,
	}
}

func (f *FeaturesHandler) Apply() error {
	if err := f.featuresProvider(f); err != nil {
		return fmt.Errorf("apply phase failed when wiring Feature instances: %w", err)
	}

	var applyErrors *multierror.Error
	for _, f := range f.features {
		applyErrors = multierror.Append(applyErrors, f.Apply())
	}

	return applyErrors.ErrorOrNil()
}

// Delete executes registered clean-up tasks in the opposite order they were initiated (following a stack structure).
// For instance, this allows for the undoing patches before its deletion.
// This approach assumes that Features are either instantiated in the correct sequence
// or are self-contained.
func (f *FeaturesHandler) Delete() error {
	if err := f.featuresProvider(f); err != nil {
		return fmt.Errorf("delete phase failed when wiring Feature instances: %w", err)
	}

	var cleanupErrors *multierror.Error
	for i := len(f.features) - 1; i >= 0; i-- {
		cleanupErrors = multierror.Append(cleanupErrors, f.features[i].Cleanup())
	}

	return cleanupErrors.ErrorOrNil()
}
