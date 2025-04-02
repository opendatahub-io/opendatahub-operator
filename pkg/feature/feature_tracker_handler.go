package feature

import (
	"context"
	"errors"
	"fmt"

	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/api/features/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
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
func createFeatureTracker(ctx context.Context, cli client.Client, f *Feature) error {
	tracker, errGet := getFeatureTracker(ctx, cli, f.Name, f.TargetNamespace)
	if client.IgnoreNotFound(errGet) != nil {
		return errGet
	}

	if k8serr.IsNotFound(errGet) {
		tracker = featurev1.NewFeatureTracker(f.Name, f.TargetNamespace)
	}

	tracker.Spec = featurev1.FeatureTrackerSpec{
		Source:       *f.source,
		AppNamespace: f.TargetNamespace,
	}

	if f.owner != nil {
		var ownerRef cluster.MetaOptions
		if f.controller {
			ownerRef = cluster.ControlledBy(f.owner, cli.Scheme())
		} else {
			ownerRef = cluster.OwnedBy(f.owner, cli.Scheme())
		}

		if errMetaOpts := cluster.ApplyMetaOptions(tracker, ownerRef); errMetaOpts != nil {
			return fmt.Errorf("failed adding owner to FeatureTracker %s: %w", tracker.Name, errMetaOpts)
		}
	}

	if k8serr.IsNotFound(errGet) {
		if errCreate := cli.Create(ctx, tracker); errCreate != nil {
			return fmt.Errorf("failed creating FeatureTracker %s: %w", tracker.Name, errCreate)
		}
	} else {
		if errUpdate := cli.Update(ctx, tracker); errUpdate != nil {
			return fmt.Errorf("failed updating FeatureTracker %s: %w", tracker.Name, errUpdate)
		}
	}

	if errGVK := ensureGVKSet(tracker, cli.Scheme()); errGVK != nil {
		return fmt.Errorf("failed ensuring GVK is set for %s: %w", tracker.Name, errGVK)
	}

	f.tracker = tracker

	return nil
}

// removeFeatureTracker removes the FeatureTracker associated with the provided Feature instance if one exists in the cluster.
func removeFeatureTracker(f *Feature) CleanupFunc {
	return func(ctx context.Context, cli client.Client) error {
		associatedTracker := f.tracker
		if associatedTracker == nil {
			// Check if it is persisted in the cluster, but Feature do not have it attached
			if tracker, errGet := getFeatureTracker(ctx, cli, f.Name, f.TargetNamespace); client.IgnoreNotFound(errGet) != nil {
				return errGet
			} else {
				associatedTracker = tracker
			}
		}

		if associatedTracker != nil {
			return client.IgnoreNotFound(cli.Delete(ctx, associatedTracker))
		}

		return nil
	}
}

func getFeatureTracker(ctx context.Context, cli client.Client, featureName, namespace string) (*featurev1.FeatureTracker, error) {
	tracker := featurev1.NewFeatureTracker(featureName, namespace)

	if errGet := cli.Get(ctx, client.ObjectKeyFromObject(tracker), tracker); errGet != nil {
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

func createFeatureTrackerStatusReporter(cli client.Client, f *Feature) *status.Reporter[*featurev1.FeatureTracker] {
	return status.NewStatusReporter(cli, f.tracker, func(err error) status.SaveStatusFunc[*featurev1.FeatureTracker] {
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
