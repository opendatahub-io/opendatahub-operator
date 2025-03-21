//nolint:structcheck // Reason: false positive, complains about unused fields in HandlerWithReporter
package feature

import (
	"context"
	"fmt"

	"github.com/hashicorp/go-multierror"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/api/features/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
)

type featuresHandler interface {
	Apply(ctx context.Context, cli client.Client) error
	Delete(ctx context.Context, cli client.Client) error
}

type FeaturesRegistry interface {
	Add(builders ...*featureBuilder) error
}

var _ featuresHandler = (*FeaturesHandler)(nil)

// FeaturesHandler provides a structured way to manage and coordinate the creation, application,
// and deletion of features needed in particular Data Science Cluster configuration.
type FeaturesHandler struct {
	source            featurev1.Source
	owner             metav1.Object
	controller        bool
	targetNamespace   string
	features          []*Feature
	featuresProviders []FeaturesProvider
}

var _ FeaturesRegistry = (*FeaturesHandler)(nil)

// Add loads features defined by passed builders and adds to internal list which is then used to Apply on the cluster.
// It also makes sure that both TargetNamespace and Source are added to the feature before it's `Create()`ed.
func (fh *FeaturesHandler) Add(builders ...*featureBuilder) error {
	var multiErr *multierror.Error

	for i := range builders {
		fb := builders[i]
		feature, err := fb.
			TargetNamespace(fh.targetNamespace).
			OwnedBy(fh.owner).
			Controller(fh.controller).
			Source(fh.source).
			Create()
		multiErr = multierror.Append(multiErr, err)
		fh.features = append(fh.features, feature)
	}

	return multiErr.ErrorOrNil()
}

func (fh *FeaturesHandler) Apply(ctx context.Context, cli client.Client) error {
	fh.features = make([]*Feature, 0)

	for _, featuresProvider := range fh.featuresProviders {
		if err := featuresProvider(fh); err != nil {
			return fmt.Errorf("failed adding features to the handler. cause: %w", err)
		}
	}

	var multiErr *multierror.Error
	for _, f := range fh.features {
		if applyErr := f.Apply(ctx, cli); applyErr != nil {
			multiErr = multierror.Append(multiErr, fmt.Errorf("failed applying FeatureHandler features. cause: %w", applyErr))
		}
	}

	return multiErr.ErrorOrNil()
}

// Delete executes registered clean-up tasks for handled Features in the opposite order they were initiated.
// This approach assumes that Features are either instantiated in the correct sequence or are self-contained.
func (fh *FeaturesHandler) Delete(ctx context.Context, cli client.Client) error {
	fh.features = make([]*Feature, 0)

	for _, featuresProvider := range fh.featuresProviders {
		if err := featuresProvider(fh); err != nil {
			return fmt.Errorf("delete phase failed when wiring Feature instances in FeatureHandler.Delete. cause: %w", err)
		}
	}

	var multiErr *multierror.Error
	for i := len(fh.features) - 1; i >= 0; i-- {
		if cleanupErr := fh.features[i].Cleanup(ctx, cli); cleanupErr != nil {
			multiErr = multierror.Append(multiErr, fmt.Errorf("failed executing cleanup in FeatureHandler. cause: %w", cleanupErr))
		}
	}

	return multiErr.ErrorOrNil()
}

// FeaturesProvider is a function which allow to define list of features
// and add them to the handler's registry.
type FeaturesProvider func(registry FeaturesRegistry) error

func ClusterFeaturesHandler(dsci *dsciv1.DSCInitialization, def ...FeaturesProvider) *FeaturesHandler {
	return &FeaturesHandler{
		targetNamespace:   dsci.Spec.ApplicationsNamespace,
		owner:             dsci,
		source:            featurev1.Source{Type: featurev1.DSCIType, Name: dsci.Name},
		featuresProviders: def,
	}
}

func ComponentFeaturesHandler(owner metav1.Object, componentName, targetNamespace string, def ...FeaturesProvider) *FeaturesHandler {
	return &FeaturesHandler{
		owner:             owner,
		controller:        true,
		targetNamespace:   targetNamespace,
		source:            featurev1.Source{Type: featurev1.ComponentType, Name: componentName},
		featuresProviders: def,
	}
}

// EmptyFeaturesHandler is noop handler so that we can avoid nil checks in the code and safely call Apply/Delete methods.
var EmptyFeaturesHandler = &FeaturesHandler{
	features:          []*Feature{},
	featuresProviders: []FeaturesProvider{},
}

// HandlerWithReporter is a wrapper around FeaturesHandler and status.Reporter
// It is intended apply features related to a given resource capabilities and report its status using custom reporter.
type HandlerWithReporter[T client.Object] struct {
	handler  *FeaturesHandler
	reporter *status.Reporter[T]
}

var _ featuresHandler = (*HandlerWithReporter[client.Object])(nil)

func NewHandlerWithReporter[T client.Object](handler *FeaturesHandler, reporter *status.Reporter[T]) *HandlerWithReporter[T] {
	return &HandlerWithReporter[T]{
		handler:  handler,
		reporter: reporter,
	}
}

func (h HandlerWithReporter[T]) Apply(ctx context.Context, cli client.Client) error {
	applyErr := h.handler.Apply(ctx, cli)
	_, reportErr := h.reporter.ReportCondition(ctx, applyErr)
	// We could have failed during Apply phase as well as during reporting.
	// We should return both errors to the caller.
	return multierror.Append(applyErr, reportErr).ErrorOrNil()
}

func (h HandlerWithReporter[T]) Delete(ctx context.Context, cli client.Client) error {
	deleteErr := h.handler.Delete(ctx, cli)
	_, reportErr := h.reporter.ReportCondition(ctx, deleteErr)
	// We could have failed during Delete phase as well as during reporting.
	// We should return both errors to the caller.
	return multierror.Append(deleteErr, reportErr).ErrorOrNil()
}
