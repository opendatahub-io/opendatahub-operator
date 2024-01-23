package feature

import (
	"context"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
)

// createFeatureTracker instantiates FeatureTracker for a given Feature. It's a cluster-scoped resource used
// to track creation and removal of all owned resources which belong to this Feature.
// All resources which particular feature is composed of will have this object attached as an OwnerReference.
func (f *Feature) createFeatureTracker() error {
	tracker, err := f.getFeatureTracker()
	if k8serrors.IsNotFound(err) {
		if err := f.Client.Create(context.TODO(), tracker); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	if err := f.ensureGVKSet(tracker); err != nil {
		return err
	}

	f.Spec.Tracker = tracker

	return nil
}

func removeFeatureTracker(f *Feature) error {
	if f.Spec.Tracker != nil {
		return deleteTracker(f)
	}

	if err := setFeatureTrackerIfAbsent(f); err != nil {
		if k8serrors.IsNotFound(err) {
			// There is nothing to delete
			return nil
		}
		return err
	}

	return deleteTracker(f)
}

func (f *Feature) getFeatureTracker() (*featurev1.FeatureTracker, error) {
	tracker := &featurev1.FeatureTracker{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "features.opendatahub.io/v1",
			Kind:       "FeatureTracker",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: f.Spec.AppNamespace + "-" + common.TrimToRFC1123Name(f.Name),
		},
	}

	err := f.Client.Get(context.Background(), client.ObjectKeyFromObject(tracker), tracker)

	return tracker, err
}

func setFeatureTrackerIfAbsent(f *Feature) error {
	tracker, err := f.getFeatureTracker()

	f.Spec.Tracker = tracker

	return err
}

func (f *Feature) ensureGVKSet(obj runtime.Object) error {
	// See https://github.com/kubernetes/client-go/issues/308
	gvks, unversioned, err := f.Client.Scheme().ObjectKinds(obj)
	if err != nil {
		return fmt.Errorf("failed to get group, version, & kinds for object: %w", err)
	}
	if unversioned {
		return fmt.Errorf("object is unversioned")
	}
	// Update the target object back with one of the discovered GVKs.
	obj.GetObjectKind().SetGroupVersionKind(gvks[0])

	return nil
}

func deleteTracker(f *Feature) error {
	err := f.Client.Delete(context.Background(), f.Spec.Tracker)
	if err != nil && !k8serrors.IsNotFound(err) {
		return err
	}
	return nil
}
