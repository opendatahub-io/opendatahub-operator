package feature

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/hashicorp/go-multierror"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/api/features/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature/resource"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/annotations"
)

// Feature is a high-level abstraction that represents a collection of resources and actions
// that are applied to the cluster to enable a specific feature.
//
// Features can be either managed or unmanaged. Managed features are reconciled to their
// desired state based on defined manifests.
//
// In addition to creating resources using manifest files or through Golang functions, a Feature
// allows defining preconditions and postconditions. These conditions are checked to ensure
// the cluster is in the desired state for the feature to be applied successfully.
//
// When a Feature is applied, an associated resource called FeatureTracker is created. This
// resource establishes ownership for related resources, allowing for easy cleanup of all resources
// associated with the feature when it is about to be removed during reconciliation.
//
// Each Feature can have a list of cleanup functions. These functions can be particularly useful
// when the cleanup involves actions other than the removal of resources, such as reverting a patch operation.
//
// To create a Feature, use the provided FeatureBuilder. This builder guides through the process
// using a fluent API.
type Feature struct {
	Name            string
	TargetNamespace string
	Enabled         EnabledFunc
	Managed         bool

	Log logr.Logger

	tracker    *featurev1.FeatureTracker
	source     *featurev1.Source
	owner      metav1.Object
	controller bool

	data map[string]any

	appliers []resource.Applier

	cleanups          []CleanupFunc
	clusterOperations []Action
	preconditions     []Action
	postconditions    []Action
	dataProviders     []Action
}

// Action is a func type which can be used for different purposes during Feature's lifecycle
// while having access to Feature struct.
type Action func(ctx context.Context, cli client.Client, f *Feature) error

// CleanupFunc defines how to clean up resources associated with a feature.
// By default, all resources created by the feature are deleted when the feature is,
// so there is no need to explicitly add cleanup hooks for them.
// This is useful when you need to perform some additional cleanup actions such as removing effects of a patch operation.
type CleanupFunc func(ctx context.Context, cli client.Client) error

// EnabledFunc is a func type used to determine if a feature should be enabled.
type EnabledFunc func(ctx context.Context, cli client.Client, feature *Feature) (bool, error)

// Apply applies the feature to the cluster.
// It creates a FeatureTracker resource to establish ownership and reports the result of the operation as a condition.
func (f *Feature) Apply(ctx context.Context, cli client.Client) error {
	// If the feature is disabled, but the FeatureTracker exists in the cluster, ensure clean-up is triggered.
	// This means that the feature was previously enabled, but now it is not anymore.
	if enabled, err := f.Enabled(ctx, cli, f); !enabled || err != nil {
		if err != nil {
			return err
		}

		return f.Cleanup(ctx, cli)
	}

	if trackerErr := createFeatureTracker(ctx, cli, f); trackerErr != nil {
		return trackerErr
	}

	if _, updateErr := status.UpdateWithRetry(ctx, cli, f.tracker, func(saved *featurev1.FeatureTracker) {
		status.SetProgressingCondition(&saved.Status.Conditions, string(featurev1.ConditionReason.FeatureCreated), fmt.Sprintf("Applying feature [%s]", f.Name))
		saved.Status.Phase = status.PhaseProgressing
	}); updateErr != nil {
		return updateErr
	}

	applyErr := f.applyFeature(ctx, cli)
	_, reportErr := createFeatureTrackerStatusReporter(cli, f).ReportCondition(ctx, applyErr)

	return multierror.Append(applyErr, reportErr).ErrorOrNil()
}

func (f *Feature) applyFeature(ctx context.Context, cli client.Client) error {
	var multiErr *multierror.Error

	for _, dataProvider := range f.dataProviders {
		multiErr = multierror.Append(multiErr, dataProvider(ctx, cli, f))
	}
	if errDataLoad := multiErr.ErrorOrNil(); errDataLoad != nil {
		return &withConditionReasonError{reason: featurev1.ConditionReason.LoadTemplateData, err: errDataLoad}
	}

	for _, precondition := range f.preconditions {
		multiErr = multierror.Append(multiErr, precondition(ctx, cli, f))
	}
	if preconditionsErr := multiErr.ErrorOrNil(); preconditionsErr != nil {
		return &withConditionReasonError{reason: featurev1.ConditionReason.PreConditions, err: preconditionsErr}
	}

	for _, clusterOperation := range f.clusterOperations {
		if errClusterOperation := clusterOperation(ctx, cli, f); errClusterOperation != nil {
			return &withConditionReasonError{reason: featurev1.ConditionReason.ResourceCreation, err: errClusterOperation}
		}
	}

	for i := range f.appliers {
		r := f.appliers[i]
		if processErr := r.Apply(ctx, cli, f.data, DefaultMetaOptions(f)...); processErr != nil {
			return &withConditionReasonError{reason: featurev1.ConditionReason.ApplyManifests, err: processErr}
		}
	}

	for _, postcondition := range f.postconditions {
		multiErr = multierror.Append(multiErr, postcondition(ctx, cli, f))
	}
	if postConditionErr := multiErr.ErrorOrNil(); postConditionErr != nil {
		return &withConditionReasonError{reason: featurev1.ConditionReason.PostConditions, err: postConditionErr}
	}

	return nil
}

func (f *Feature) Cleanup(ctx context.Context, cli client.Client) error {
	// Ensure associated FeatureTracker instance has been removed as last one
	// in the chain of cleanups.
	f.addCleanup(removeFeatureTracker(f))

	var cleanupErrors *multierror.Error
	for _, cleanupFunc := range f.cleanups {
		cleanupErrors = multierror.Append(cleanupErrors, cleanupFunc(ctx, cli))
	}

	return cleanupErrors.ErrorOrNil()
}

func (f *Feature) addCleanup(cleanupFuncs ...CleanupFunc) {
	f.cleanups = append(f.cleanups, cleanupFuncs...)
}

// AsOwnerReference returns an OwnerReference for the FeatureTracker resource.
func (f *Feature) AsOwnerReference() metav1.OwnerReference {
	return f.tracker.ToOwnerReference()
}

// OwnedBy returns a cluster.MetaOptions that sets the owner reference to the FeatureTracker resource.
func OwnedBy(f *Feature) cluster.MetaOptions {
	return cluster.WithOwnerReference(f.AsOwnerReference())
}

func ControlledBy(f *Feature) cluster.MetaOptions {
	or := f.AsOwnerReference()
	or.Controller = ptr.To[bool](true)
	or.BlockOwnerDeletion = ptr.To[bool](true)
	return cluster.WithOwnerReference(or)
}

func DefaultMetaOptions(f *Feature) []cluster.MetaOptions {
	resourceMeta := make([]cluster.MetaOptions, 0, 1)

	if f.controller {
		resourceMeta = append(resourceMeta, ControlledBy(f))
	} else {
		resourceMeta = append(resourceMeta, OwnedBy(f))
	}

	if f.Managed {
		resourceMeta = append(resourceMeta, func(obj metav1.Object) error {
			objAnnotations := obj.GetAnnotations()
			if objAnnotations == nil {
				objAnnotations = make(map[string]string)
			}

			// If resource already has an annotation, it takes precedence
			if _, exists := objAnnotations[annotations.ManagedByODHOperator]; !exists {
				objAnnotations[annotations.ManagedByODHOperator] = "true"
				obj.SetAnnotations(objAnnotations)
			}

			return nil
		})
	}
	return resourceMeta
}
