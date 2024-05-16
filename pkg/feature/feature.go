package feature

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/go-logr/logr"
	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"

	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

type Feature struct {
	Name    string
	Spec    *Spec
	Enabled bool
	Managed bool
	Tracker *featurev1.FeatureTracker

	Client client.Client

	manifests []Manifest

	cleanups       []Action
	resources      []Action
	preconditions  []Action
	postconditions []Action
	loaders        []Action
	fsys           fs.FS

	Log logr.Logger
}

func newFeature(name string) *Feature {
	return &Feature{
		Name:    name,
		Enabled: true,
		Log:     ctrlLog.Log.WithName("features").WithValues("feature", name),
	}
}

// Action is a func type which can be used for different purposes while having access to Feature struct.
type Action func(feature *Feature) error

func (f *Feature) Apply() error {
	if !f.Enabled {
		return nil
	}

	if trackerErr := f.createFeatureTracker(); trackerErr != nil {
		return trackerErr
	}

	if _, updateErr := status.UpdateWithRetry(context.Background(), f.Client, f.Tracker, func(saved *featurev1.FeatureTracker) {
		status.SetProgressingCondition(&saved.Status.Conditions, string(featurev1.ConditionReason.FeatureCreated), fmt.Sprintf("Applying feature [%s]", f.Name))
		saved.Status.Phase = status.PhaseProgressing
	}); updateErr != nil {
		return updateErr
	}

	applyErr := f.applyFeature()
	_, reportErr := createFeatureTrackerStatusReporter(f).ReportCondition(applyErr)

	return multierror.Append(applyErr, reportErr).ErrorOrNil()
}

func (f *Feature) applyFeature() error {
	var multiErr *multierror.Error

	for _, precondition := range f.preconditions {
		multiErr = multierror.Append(multiErr, precondition(f))
	}

	if preconditionsErr := multiErr.ErrorOrNil(); preconditionsErr != nil {
		return &withConditionReasonError{reason: featurev1.ConditionReason.PreConditions, err: preconditionsErr}
	}

	for _, loader := range f.loaders {
		multiErr = multierror.Append(multiErr, loader(f))
	}
	if dataLoadErr := multiErr.ErrorOrNil(); dataLoadErr != nil {
		return &withConditionReasonError{reason: featurev1.ConditionReason.LoadTemplateData, err: dataLoadErr}
	}

	for _, resource := range f.resources {
		if resourceCreateErr := resource(f); resourceCreateErr != nil {
			return &withConditionReasonError{reason: featurev1.ConditionReason.ResourceCreation, err: resourceCreateErr}
		}
	}

	for i := range f.manifests {
		manifest := f.manifests[i]
		apply := f.createApplier(manifest)

		objs, processErr := manifest.Process(f.Spec)
		if processErr != nil {
			return &withConditionReasonError{reason: featurev1.ConditionReason.ApplyManifests, err: processErr}
		}

		if f.Managed {
			manifest.MarkAsManaged(objs)
		}

		if err := apply(objs); err != nil {
			return &withConditionReasonError{reason: featurev1.ConditionReason.ApplyManifests, err: err}
		}
	}

	for _, postcondition := range f.postconditions {
		multiErr = multierror.Append(multiErr, postcondition(f))
	}
	if postConditionErr := multiErr.ErrorOrNil(); postConditionErr != nil {
		return &withConditionReasonError{reason: featurev1.ConditionReason.PostConditions, err: postConditionErr}
	}

	return nil
}

func (f *Feature) Cleanup() error {
	if !f.Enabled {
		return nil
	}

	// Ensure associated FeatureTracker instance has been removed as last one
	// in the chain of cleanups.
	f.addCleanup(removeFeatureTracker)

	var cleanupErrors *multierror.Error
	for _, cleanupFunc := range f.cleanups {
		cleanupErrors = multierror.Append(cleanupErrors, cleanupFunc(f))
	}

	return cleanupErrors.ErrorOrNil()
}

type applier func(objects []*unstructured.Unstructured) error

func (f *Feature) createApplier(m Manifest) applier {
	switch manifest := m.(type) {
	case *templateManifest:
		if manifest.patch {
			return func(objects []*unstructured.Unstructured) error {
				return patchResources(f.Client, objects)
			}
		}
	case *rawManifest:
		if manifest.patch {
			return func(objects []*unstructured.Unstructured) error {
				return patchResources(f.Client, objects)
			}
		}
	}

	return func(objects []*unstructured.Unstructured) error {
		return applyResources(f.Client, objects, OwnedBy(f))
	}
}

func (f *Feature) addCleanup(cleanupFuncs ...Action) {
	f.cleanups = append(f.cleanups, cleanupFuncs...)
}

func (f *Feature) ApplyManifest(path string) error {
	m, err := loadManifestsFrom(f.fsys, path)
	if err != nil {
		return err
	}
	for i := range m {
		var objs []*unstructured.Unstructured
		manifest := m[i]
		apply := f.createApplier(manifest)

		if objs, err = manifest.Process(f.Spec); err != nil {
			return errors.WithStack(err)
		}

		if f.Managed {
			manifest.MarkAsManaged(objs)
		}

		if err = apply(objs); err != nil {
			return errors.WithStack(err)
		}
	}
	return nil
}

func (f *Feature) AsOwnerReference() metav1.OwnerReference {
	return f.Tracker.ToOwnerReference()
}

func OwnedBy(f *Feature) cluster.MetaOptions {
	return cluster.WithOwnerReference(f.AsOwnerReference())
}
