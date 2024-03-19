package feature

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/go-logr/logr"
	"github.com/hashicorp/go-multierror"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"

	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

type Feature struct {
	Name    string
	Spec    *Spec
	Enabled bool
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

//nolint:nonamedreturns // Reason: we use the named return to handle errors in a unified fashion through deferred function.
func (f *Feature) Apply() (err error) {
	if !f.Enabled {
		return nil
	}

	if trackerErr := f.createFeatureTracker(); trackerErr != nil {
		return trackerErr
	}

	// Verify all precondition and collect errors
	var multiErr *multierror.Error
	phase := featurev1.FeatureCreated
	f.updateFeatureTrackerStatus(conditionsv1.ConditionProgressing, "True", phase, fmt.Sprintf("Applying feature %s", f.Name))
	defer func() {
		if err != nil {
			f.updateFeatureTrackerStatus(conditionsv1.ConditionProgressing, "False", phase, fmt.Sprintf("Feature %s finished", f.Name))
			f.updateFeatureTrackerStatus(conditionsv1.ConditionDegraded, "True", phase, err.Error())
		} else {
			f.updateFeatureTrackerStatus(conditionsv1.ConditionProgressing, "False", phase, fmt.Sprintf("Feature %s finished", f.Name))
			f.updateFeatureTrackerStatus(conditionsv1.ConditionAvailable, "True", phase, fmt.Sprintf("Feature %s applied successfully", f.Name))
		}
	}()

	phase = featurev1.PreConditions
	for _, precondition := range f.preconditions {
		multiErr = multierror.Append(multiErr, precondition(f))
	}

	if preconditionsErr := multiErr.ErrorOrNil(); preconditionsErr != nil {
		return preconditionsErr
	}

	phase = featurev1.LoadTemplateData
	for _, loader := range f.loaders {
		multiErr = multierror.Append(multiErr, loader(f))
	}

	if dataLoadErr := multiErr.ErrorOrNil(); dataLoadErr != nil {
		return dataLoadErr
	}

	phase = featurev1.ResourceCreation
	for _, resource := range f.resources {
		if err := resource(f); err != nil {
			return errors.WithStack(err)
		}
	}

	phase = featurev1.ApplyManifests
	for i := range f.manifests {
		var objs []*unstructured.Unstructured
		manifest := f.manifests[i]
		apply := f.createApplier(manifest)

		if objs, err = manifest.Process(f.Spec); err != nil {
			return errors.WithStack(err)
		}

		if err = apply(objs); err != nil {
			return errors.WithStack(err)
		}
	}

	phase = featurev1.PostConditions
	for _, postcondition := range f.postconditions {
		multiErr = multierror.Append(multiErr, postcondition(f))
	}
	if multiErr.ErrorOrNil() != nil {
		return multiErr.ErrorOrNil()
	}

	phase = featurev1.FeatureCreated
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
		return createResources(f.Client, objects, OwnedBy(f))
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

// updateFeatureTrackerStatus updates conditions of a FeatureTracker.
// It's deliberately logging errors instead of handing them as it is used in deferred error handling of Feature public API,
// which is more predictable.
func (f *Feature) updateFeatureTrackerStatus(condType conditionsv1.ConditionType, status corev1.ConditionStatus, reason featurev1.FeaturePhase, message string) {
	tracker := f.Tracker

	// Update the status
	if tracker.Status.Conditions == nil {
		tracker.Status.Conditions = &[]conditionsv1.Condition{}
	}
	conditionsv1.SetStatusCondition(tracker.Status.Conditions, conditionsv1.Condition{
		Type:    condType,
		Status:  status,
		Reason:  string(reason),
		Message: message,
	})

	err := f.Client.Status().Update(context.Background(), tracker)
	if err != nil {
		f.Log.Error(err, "Error updating FeatureTracker status")
	}

	f.Tracker.Status = tracker.Status
}
