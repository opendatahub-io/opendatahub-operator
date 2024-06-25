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

// createFeatureTracker creates a FeatureTracker, persists it in the cluster,
// and attaches it to the provided Feature instance.
func createFeatureTracker(ctx context.Context, f *Feature) error {
	tracker, errGet := getFeatureTracker(ctx, f)
	if client.IgnoreNotFound(errGet) != nil {
		return errGet
	}

	if k8serr.IsNotFound(errGet) {
		tracker = featurev1.NewFeatureTracker(f.Name, f.Spec.AppNamespace)
		tracker.Spec = featurev1.FeatureTrackerSpec{
			Source:       *f.Spec.Source,
			AppNamespace: f.Spec.AppNamespace,
		}
		if errCreate := f.Client.Create(ctx, tracker); errCreate != nil {
			return errCreate
		}
	}

	if gvkErr := ensureGVKSet(tracker, f.Client.Scheme()); gvkErr != nil {
		return gvkErr
	}

	f.Tracker = tracker

	return nil
}

func removeFeatureTracker(ctx context.Context, f *Feature) error {
	associatedTracker := f.Tracker
	if associatedTracker == nil {
		// Check if it is persisted in the cluster, but Feature do not have it attached
		if tracker, errGet := getFeatureTracker(ctx, f); client.IgnoreNotFound(errGet) != nil {
			return errGet
		} else {
			associatedTracker = tracker
		}
	}

	if associatedTracker != nil {
		return client.IgnoreNotFound(f.Client.Delete(ctx, associatedTracker))
	}

	return nil
}

func getFeatureTracker(ctx context.Context, f *Feature) (*featurev1.FeatureTracker, error) {
	tracker := featurev1.NewFeatureTracker(f.Name, f.Spec.AppNamespace)

	if errGet := f.Client.Get(ctx, client.ObjectKeyFromObject(tracker), tracker); errGet != nil {
		return nil, errGet
	}

	return tracker, nil
}

func ensureGVKSet(obj runtime.Object, scheme *runtime.Scheme) error {
	// See https://github.com/kubernetes/client-go/issues/308
	gvks, unversioned, err := scheme.ObjectKinds(obj)
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
