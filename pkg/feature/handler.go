//nolint:structcheck // Reason: false positive, complains about unused fields in HandlerWithReporter
package feature

import (
	"fmt"

	"github.com/hashicorp/go-multierror"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
)

type featureHandler interface {
	Apply() error
	Delete() error
}

var _ featureHandler = (*FeaturesHandler)(nil)

// FeaturesHandler coordinates feature creations and removal from within controllers.
type FeaturesHandler struct {
	*dsciv1.DSCInitializationSpec
	source            featurev1.Source
	features          []*Feature
	featuresProviders []FeaturesProvider
}

// EmptyFeaturesHandler is noop handler so that we can avoid nil checks in the code and safely call Apply/Delete methods.
var EmptyFeaturesHandler = &FeaturesHandler{
	features:          []*Feature{},
	featuresProviders: []FeaturesProvider{},
}

var _ featureHandler = (*HandlerWithReporter[client.Object])(nil)

// HandlerWithReporter is a wrapper around FeaturesHandler and status.Reporter
// It is intended apply features related to a given resource capabilities and report its status using custom reporter.
type HandlerWithReporter[T client.Object] struct {
	handler  *FeaturesHandler
	reporter *status.Reporter[T]
}

func NewHandlerWithReporter[T client.Object](handler *FeaturesHandler, reporter *status.Reporter[T]) *HandlerWithReporter[T] {
	return &HandlerWithReporter[T]{
		handler:  handler,
		reporter: reporter,
	}
}

func (h HandlerWithReporter[T]) Apply() error {
	applyErr := h.handler.Apply()
	_, reportErr := h.reporter.ReportCondition(applyErr)
	// We could have failed during Apply phase as well as during reporting.
	// We should return both errors to the caller.
	return multierror.Append(applyErr, reportErr).ErrorOrNil()
}

func (h HandlerWithReporter[T]) Delete() error {
	deleteErr := h.handler.Delete()
	_, reportErr := h.reporter.ReportCondition(deleteErr)
	// We could have failed during Delete phase as well as during reporting.
	// We should return both errors to the caller.
	return multierror.Append(deleteErr, reportErr).ErrorOrNil()
}

// FeaturesProvider is a function which allow to define list of features
// and couple them with the given initializer.
type FeaturesProvider func(handler *FeaturesHandler) error

func ClusterFeaturesHandler(dsci *dsciv1.DSCInitialization, def ...FeaturesProvider) *FeaturesHandler {
	return &FeaturesHandler{
		DSCInitializationSpec: &dsci.Spec,
		source:                featurev1.Source{Type: featurev1.DSCIType, Name: dsci.Name},
		featuresProviders:     def,
	}
}

func ComponentFeaturesHandler(componentName string, spec *dsciv1.DSCInitializationSpec, def ...FeaturesProvider) *FeaturesHandler {
	return &FeaturesHandler{
		DSCInitializationSpec: spec,
		source:                featurev1.Source{Type: featurev1.ComponentType, Name: componentName},
		featuresProviders:     def,
	}
}

func (fh *FeaturesHandler) Apply() error {
	for _, featuresProvider := range fh.featuresProviders {
		if err := featuresProvider(fh); err != nil {
			return fmt.Errorf("apply phase failed when applying features: %w", err)
		}
	}

	var applyErrors *multierror.Error
	for _, f := range fh.features {
		applyErrors = multierror.Append(applyErrors, f.Apply())
	}

	return applyErrors.ErrorOrNil()
}

// Delete executes registered clean-up tasks in the opposite order they were initiated (following a stack structure).
// For instance, this allows for the undoing patches before its deletion.
// This approach assumes that Features are either instantiated in the correct sequence
// or are self-contained.
func (fh *FeaturesHandler) Delete() error {
	for _, featuresProvider := range fh.featuresProviders {
		if err := featuresProvider(fh); err != nil {
			return fmt.Errorf("delete phase failed when wiring Feature instances: %w", err)
		}
	}

	var cleanupErrors *multierror.Error
	for i := len(fh.features) - 1; i >= 0; i-- {
		cleanupErrors = multierror.Append(cleanupErrors, fh.features[i].Cleanup())
	}

	return cleanupErrors.ErrorOrNil()
}
