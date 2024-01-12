package feature

import (
	"context"
	"fmt"
	"io/fs"

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

var log = ctrlLog.Log.WithName("features")

type Feature struct {
	Name    string
	Spec    *Spec
	Enabled bool

	Clientset     *kubernetes.Clientset
	DynamicClient dynamic.Interface
	Client        client.Client

	fsys      fs.FS
	manifests []manifest

	cleanups       []Action
	resources      []Action
	preconditions  []Action
	postconditions []Action
	loaders        []Action
}

// Action is a func type which can be used for different purposes while having access to Feature struct.
type Action func(feature *Feature) error

func (f *Feature) Apply() (err error) { //nolint:nonamedreturns //reason we use this error value in deferred update of the status
	if !f.Enabled {
		log.Info("feature is disabled, skipping.", "feature", f.Name)

		return nil
	}

	var multiErr *multierror.Error
	var phase string
	phase = featurev1.ConditionPhaseFeatureCreated
	f.UpdateFeatureTrackerStatus(conditionsv1.ConditionDegraded, "False", phase, fmt.Sprintf("Applying feature %s", f.Name))
	defer func() {
		if err != nil {
			f.UpdateFeatureTrackerStatus(conditionsv1.ConditionDegraded, "True", phase, err.Error())
		} else {
			f.UpdateFeatureTrackerStatus(conditionsv1.ConditionAvailable, "True", phase, fmt.Sprintf("Feature %s applied successfully", f.Name))
		}
	}()

	phase = featurev1.ConditionPhasePreConditions
	for _, precondition := range f.preconditions {
		multiErr = multierror.Append(multiErr, precondition(f))
	}

	if multiErr.ErrorOrNil() != nil {
		return multiErr.ErrorOrNil()
	}

	phase = featurev1.ConditionPhaseLoadTemplateData
	for _, loader := range f.loaders {
		multiErr = multierror.Append(multiErr, loader(f))
	}
	if multiErr.ErrorOrNil() != nil {
		return multiErr.ErrorOrNil()
	}

	phase = featurev1.ConditionPhaseResourceCreation
	for _, resource := range f.resources {
		if err := resource(f); err != nil {
			return err
		}
	}

	phase = featurev1.ConditionPhaseProcessTemplates
	for i, m := range f.manifests {
		if err := m.processTemplate(f.fsys, f.Spec); err != nil {
			return errors.WithStack(err)
		}

		f.manifests[i] = m
	}

	phase = featurev1.ConditionPhaseApplyManifests
	if err := f.applyManifests(); err != nil {
		return err
	}

	phase = featurev1.ConditionPhasePostConditions
	for _, postcondition := range f.postconditions {
		multiErr = multierror.Append(multiErr, postcondition(f))
	}
	if multiErr.ErrorOrNil() != nil {
		return multiErr.ErrorOrNil()
	}

	phase = featurev1.ConditionPhaseFeatureCreated
	return nil
}

func (f *Feature) Cleanup() error {
	if !f.Enabled {
		log.Info("feature is disabled, skipping.", "feature", f.Name)

		return nil
	}

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
				f.OwnerReference(),
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

func (f *Feature) ApplyManifest(filename string) error {
	m := loadManifestFrom(filename)
	if err := m.processTemplate(f.fsys, f.Spec); err != nil {
		return err
	}

	return f.apply(m)
}

type apply func(data string) error

func (f *Feature) apply(m manifest) error {
	var applier apply
	targetPath := m.targetPath()

	if m.patch {
		applier = func(data string) error {
			log.Info("patching using manifest", "feature", f.Name, "name", m.name, "path", targetPath)

			return f.patchResources(data)
		}
	} else {
		applier = func(data string) error {
			log.Info("applying manifest", "feature", f.Name, "name", m.name, "path", targetPath)

			return f.createResources(data)
		}
	}

	if err := applier(m.processedContent); err != nil {
		log.Error(err, "failed to create resource", "feature", f.Name, "name", m.name, "path", targetPath)

		return err
	}

	return nil
}

func (f *Feature) OwnerReference() metav1.OwnerReference {
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
		Spec: featurev1.FeatureTrackerSpec{
			Origin:       *f.Spec.Origin,
			AppNamespace: f.Spec.AppNamespace,
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
		if err := f.DynamicClient.Resource(gvr.FeatureTracker).Delete(context.TODO(), f.Spec.Tracker.Name, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
			return err
		}

		return nil
	})

	return nil
}

func (f *Feature) UpdateFeatureTrackerStatus(condType conditionsv1.ConditionType, status corev1.ConditionStatus, reason, message string) {
	if f.Spec.Tracker.Status.Conditions == nil {
		f.Spec.Tracker.Status.Conditions = &[]conditionsv1.Condition{}
	}

	conditionsv1.SetStatusCondition(f.Spec.Tracker.Status.Conditions, conditionsv1.Condition{
		Type:    condType,
		Status:  status,
		Reason:  reason,
		Message: message,
	})

	modifiedTracker, err := runtime.DefaultUnstructuredConverter.ToUnstructured(f.Spec.Tracker)
	if err != nil {
		log.Error(err, "Error converting modified FeatureTracker to unstructured")
		return
	}

	u := unstructured.Unstructured{Object: modifiedTracker}
	updated, err := f.DynamicClient.Resource(gvr.FeatureTracker).Update(context.TODO(), &u, metav1.UpdateOptions{})
	if err != nil {
		log.Error(err, "Error updating FeatureTracker status")
	}

	var updatedTracker featurev1.FeatureTracker
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(updated.Object, &updatedTracker); err != nil {
		log.Error(err, "Error converting updated unstructured object to FeatureTracker")
		return
	}

	f.Spec.Tracker = &updatedTracker
}
