package feature

import (
	"context"
	"errors"
	"fmt"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
)

// withConditionReasonError is a wrapper around an error which provides a reason for a feature condition.
type withConditionReasonError struct {
	reason featurev1.FeatureConditionReason
	err    error
}

func (e *withConditionReasonError) Unwrap() error {
	return e.err
}

func (e *withConditionReasonError) Error() string {
	return e.err.Error()
}

// createFeatureTracker instantiates FeatureTracker for a given Feature. It's a cluster-scoped resource used
// to track creation and removal of all owned resources which belong to this Feature.
// All resources which particular feature is composed of will have this object attached as an OwnerReference.
func (f *Feature) createFeatureTracker(ctx context.Context) error {
	tracker, err := f.getFeatureTracker(ctx)
	if k8serr.IsNotFound(err) {
		if err := f.Client.Create(ctx, tracker); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	if gvkErr := f.ensureGVKSet(tracker); gvkErr != nil {
		return gvkErr
	}

	f.Tracker = tracker

	return nil
}

func removeFeatureTracker(ctx context.Context, f *Feature) error {
	if err := getFeatureTrackerIfAbsent(ctx, f); err != nil {
		return client.IgnoreNotFound(err)
	}

	return deleteTracker(ctx, f)
}

func (f *Feature) getFeatureTracker(ctx context.Context) (*featurev1.FeatureTracker, error) {
	tracker := featurev1.NewFeatureTracker(f.Name, f.Spec.AppNamespace)

	tracker.Spec = featurev1.FeatureTrackerSpec{
		Source:       *f.Spec.Source,
		AppNamespace: f.Spec.AppNamespace,
	}

	err := f.Client.Get(ctx, client.ObjectKeyFromObject(tracker), tracker)

	return tracker, err
}

func deleteTracker(ctx context.Context, f *Feature) error {
	return client.IgnoreNotFound(f.Client.Delete(ctx, f.Tracker))
}

func getFeatureTrackerIfAbsent(ctx context.Context, f *Feature) error {
	var err error
	f.Tracker, err = f.getFeatureTracker(ctx)
	return err
}

func (f *Feature) ensureGVKSet(obj runtime.Object) error {
	// See https://github.com/kubernetes/client-go/issues/308
	gvks, unversioned, err := f.Client.Scheme().ObjectKinds(obj)
	if err != nil {
		return fmt.Errorf("failed to get group, version, & kinds for object: %w", err)
	}
	if unversioned {
		return errors.New("object is unversioned")
	}
	// Update the target object back with one of the discovered GVKs.
	obj.GetObjectKind().SetGroupVersionKind(gvks[0])

	return nil
}

func createFeatureTrackerStatusReporter(f *Feature) *status.Reporter[*featurev1.FeatureTracker] {
	return status.NewStatusReporter(f.Client, f.Tracker, func(err error) status.SaveStatusFunc[*featurev1.FeatureTracker] {
		updatedCondition := func(saved *featurev1.FeatureTracker) {
			status.SetCompleteCondition(&saved.Status.Conditions, string(featurev1.ConditionReason.FeatureCreated), fmt.Sprintf("Applied feature [%s] successfully", f.Name))
			saved.Status.Phase = status.PhaseReady
		}
		if err != nil {
			reason := featurev1.ConditionReason.FailedApplying // generic reason when error is not related to any specific step of the feature apply
			var conditionErr *withConditionReasonError
			if errors.As(err, &conditionErr) {
				reason = conditionErr.reason
			}
			updatedCondition = func(saved *featurev1.FeatureTracker) {
				status.SetErrorCondition(&saved.Status.Conditions, string(reason), fmt.Sprintf("Failed applying [%s]: %+v", f.Name, err))
				saved.Status.Phase = status.PhaseError
			}
		}

		return updatedCondition
	})
}
