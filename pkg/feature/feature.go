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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"

	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/gvr"
)

type Feature struct {
	Name    string
	Spec    *Spec
	Enabled bool

	Clientset     *kubernetes.Clientset
	DynamicClient dynamic.Interface
	Client        client.Client

	manifests      []manifest
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

//nolint:nonamedreturns // Reason: we use the named return for our defer piece, needed for that.
func (f *Feature) Apply() (err error) {
	if !f.Enabled {
		f.Log.Info("feature is disabled, skipping")

		return nil
	}

	if trackerErr := f.createFeatureTracker(); err != nil {
		return trackerErr
	}

	// Verify all precondition and collect errors
	var multiErr *multierror.Error
	var phase featurev1.ConditionPhase
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
	for _, m := range f.manifests {
		if err := m.processTemplate(f.Spec); err != nil {
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
		f.Log.Info("feature is disabled, skipping")

		return nil
	}

	var cleanupErrors *multierror.Error
	for _, cleanupFunc := range f.cleanups {
		cleanupErrors = multierror.Append(cleanupErrors, cleanupFunc(f))
	}

	cleanupErr := cleanupErrors.ErrorOrNil()
	if cleanupErr != nil {
		f.Log.Error(cleanupErr, "failed performing feature cleanup")
	}

	return cleanupErr
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

type apply func(filename string) error

func (f *Feature) apply(m manifest) error {
	var applier apply
	targetPath := m.targetPath()

	if m.patch {
		applier = func(filename string) error {
			f.Log.Info("patching using manifest", "feature", f.Name, "name", m.name, "path", targetPath)

			return f.patchResourceFromFile(filename)
		}
	} else {
		applier = func(filename string) error {
			f.Log.Info("applying manifest", "feature", f.Name, "name", m.name, "path", targetPath)

			return f.createResourceFromFile(filename)
		}
	}

	if err := applier(targetPath); err != nil {
		f.Log.Error(err, "failed to create resource", "feature", f.Name, "name", m.name, "path", targetPath)

		return err
	}

	return nil
}

func (f *Feature) AsOwnerReference() metav1.OwnerReference {
	return f.Spec.Tracker.ToOwnerReference()
}

// createFeatureTracker instantiates FeatureTracker for a given Feature. All resources created when applying
// it will have this object attached as an OwnerReference.
// It's a cluster-scoped resource. Once created, there's a cleanup hook added which will be invoked on deletion, resulting
// in removal of all owned resources which belong to this Feature.
func (f *Feature) createFeatureTracker() error {
	tracker := &featurev1.FeatureTracker{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "features.opendatahub.io/v1",
			Kind:       "FeatureTracker",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: f.Spec.AppNamespace + "-" + common.TrimToRFC1123Name(f.Name),
		},
	}

	foundTracker, err := f.DynamicClient.Resource(gvr.FeatureTracker).Get(context.TODO(), tracker.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		unstructuredTracker, err := runtime.DefaultUnstructuredConverter.ToUnstructured(tracker)
		if err != nil {
			return err
		}

		u := unstructured.Unstructured{Object: unstructuredTracker}

		foundTracker, err = f.DynamicClient.Resource(gvr.FeatureTracker).Create(context.TODO(), &u, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	f.Spec.Tracker = &featurev1.FeatureTracker{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(foundTracker.Object, f.Spec.Tracker); err != nil {
		return err
	}

	// Register its own cleanup
	f.addCleanup(func(feature *Feature) error {
		err := f.DynamicClient.Resource(gvr.FeatureTracker).Delete(context.TODO(), f.Spec.Tracker.Name, metav1.DeleteOptions{})

		if !k8serrors.IsNotFound(err) {
			return err
		}

		return nil
	})

	return nil
}

func (f *Feature) UpdateFeatureTrackerStatus(condType conditionsv1.ConditionType, status corev1.ConditionStatus, reason featurev1.ConditionPhase, message string) {
	if f.Spec.Tracker.Status.Conditions == nil {
		f.Spec.Tracker.Status.Conditions = &[]conditionsv1.Condition{}
	}

	conditionsv1.SetStatusCondition(f.Spec.Tracker.Status.Conditions, conditionsv1.Condition{
		Type:    condType,
		Status:  status,
		Reason:  string(reason),
		Message: message,
	})

	modifiedTracker, err := runtime.DefaultUnstructuredConverter.ToUnstructured(f.Spec.Tracker)
	if err != nil {
		f.Log.Error(err, "Error converting modified FeatureTracker to unstructured")
		return
	}

	u := unstructured.Unstructured{Object: modifiedTracker}
	updated, err := f.DynamicClient.Resource(gvr.FeatureTracker).Update(context.TODO(), &u, metav1.UpdateOptions{})
	if err != nil {
		f.Log.Error(err, "Error updating FeatureTracker status")
	}

	var updatedTracker featurev1.FeatureTracker
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(updated.Object, &updatedTracker); err != nil {
		f.Log.Error(err, "Error converting updated unstructured object to FeatureTracker")
		return
	}

	f.Spec.Tracker = &updatedTracker
}
