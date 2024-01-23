package feature

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/hashicorp/go-multierror"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"

	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
)

type Feature struct {
	Name    string
	Spec    *Spec
	Enabled bool

	Clientset     *kubernetes.Clientset
	DynamicClient dynamic.Interface
	Client        client.Client

	manifests []manifest

	cleanups       []Action
	resources      []Action
	preconditions  []Action
	postconditions []Action
	loaders        []Action

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

	if trackerErr := f.createResourceTracker(); err != nil {
		return trackerErr
	}

	// Verify all precondition and collect errors
	var multiErr *multierror.Error
	var phase featurev1.FeaturePhase
	phase = featurev1.FeatureCreated
	f.UpdateFeatureTrackerStatus(conditionsv1.ConditionDegraded, "False", phase, fmt.Sprintf("Applying feature %s", f.Name))
	defer func() {
		if err != nil {
			f.UpdateFeatureTrackerStatus(conditionsv1.ConditionDegraded, "True", phase, err.Error())
		} else {
			f.UpdateFeatureTrackerStatus(conditionsv1.ConditionAvailable, "True", phase, fmt.Sprintf("Feature %s applied successfully", f.Name))
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

	phase = featurev1.ProcessTemplates
	for i := range f.manifests {
		if err := f.manifests[i].process(f.Spec); err != nil {
			return errors.WithStack(err)
		}
	}

	phase = featurev1.ApplyManifests
	if err := f.applyManifests(); err != nil {
		return errors.WithStack(err)
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

func (f *Feature) applyManifests() error {
	var applyErrors *multierror.Error
	for _, m := range f.manifests {
		applyErrors = multierror.Append(applyErrors, f.apply(m))
	}

	return applyErrors.ErrorOrNil()
}

func (f *Feature) CreateConfigMap(cfgMapName string, data map[string]string) error {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfgMapName,
			Namespace: f.Spec.AppNamespace,
			OwnerReferences: []metav1.OwnerReference{
				f.AsOwnerReference(),
			},
		},
		Data: data,
	}

	configMaps := f.Clientset.CoreV1().ConfigMaps(configMap.Namespace)
	_, err := configMaps.Get(context.TODO(), configMap.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) { //nolint:gocritic
		_, err = configMaps.Create(context.TODO(), configMap, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	} else if k8serrors.IsAlreadyExists(err) {
		_, err = configMaps.Update(context.TODO(), configMap, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	} else {
		return err
	}

	return nil
}

func (f *Feature) addCleanup(cleanupFuncs ...Action) {
	f.cleanups = append(f.cleanups, cleanupFuncs...)
}

type apply func(data string) error

func (f *Feature) apply(m manifest) error {
	var applier apply
	targetPath := m.targetPath()

	if m.patch {
		applier = func(data string) error {
			f.Log.Info("patching using manifest", "feature", f.Name, "name", m.name, "path", targetPath)

			return f.patchResources(data)
		}
	} else {
		applier = func(data string) error {
			f.Log.Info("applying manifest", "feature", f.Name, "name", m.name, "path", targetPath)

			return f.createResources(data)
		}
	}

	if err := applier(m.processedContent); err != nil {
		f.Log.Error(err, "failed to create resource", "feature", f.Name, "name", m.name, "path", targetPath)

		return err
	}

	return nil
}

func (f *Feature) AsOwnerReference() metav1.OwnerReference {
	return f.Spec.Tracker.ToOwnerReference()
}

func (f *Feature) UpdateFeatureTrackerStatus(condType conditionsv1.ConditionType, status corev1.ConditionStatus, reason featurev1.FeaturePhase, message string) {
	tracker := &featurev1.FeatureTracker{}
	err := f.Client.Get(context.TODO(), types.NamespacedName{
		Name: f.Spec.Tracker.Name,
	}, tracker)

	if err != nil {
		f.Log.Error(err, "Error fetching FeatureTracker")
	}

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

	err = f.Client.Status().Update(context.Background(), tracker)
	if err != nil {
		f.Log.Error(err, "Error updating FeatureTracker status")
	}

	f.Spec.Tracker.Status = tracker.Status
}
