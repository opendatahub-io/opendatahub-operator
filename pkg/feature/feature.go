package feature

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/hashicorp/go-multierror"
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

func (f *Feature) Apply() error {
	if !f.Enabled {
		f.Log.Info("feature is disabled, skipping")

		return nil
	}

	if err := f.createResourceTracker(); err != nil {
		return err
	}

	// Verify all precondition and collect errors
	var multiErr *multierror.Error
	for _, precondition := range f.preconditions {
		multiErr = multierror.Append(multiErr, precondition(f))
	}

	if preconditionsErr := multiErr.ErrorOrNil(); preconditionsErr != nil {
		return preconditionsErr
	}

	// Load necessary data
	for _, loader := range f.loaders {
		multiErr = multierror.Append(multiErr, loader(f))
	}

	if dataLoadErr := multiErr.ErrorOrNil(); dataLoadErr != nil {
		return dataLoadErr
	}

	// create or update resources
	for _, resource := range f.resources {
		if err := resource(f); err != nil {
			return errors.WithStack(err)
		}
	}

	// Process and apply manifests
	for _, m := range f.manifests {
		if err := m.processTemplate(f.Spec); err != nil {
			return errors.WithStack(err)
		}
	}

	if err := f.applyManifests(); err != nil {
		return errors.WithStack(err)
	}

	for _, postcondition := range f.postconditions {
		multiErr = multierror.Append(multiErr, postcondition(f))
	}

	return multiErr.ErrorOrNil()
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

// createResourceTracker instantiates FeatureTracker for a given Feature. All resources created when applying
// it will have this object attached as an OwnerReference.
// It's a cluster-scoped resource. Once created, there's a cleanup hook added which will be invoked on deletion, resulting
// in removal of all owned resources which belong to this Feature.
func (f *Feature) createResourceTracker() error {
	tracker := &featurev1.FeatureTracker{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "features.opendatahub.io/v1",
			Kind:       "FeatureTracker",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: f.Spec.AppNamespace + "-" + common.TrimToRFC1123Name(f.Name),
		},
	}

	foundTracker, err := f.DynamicClient.Resource(gvr.ResourceTracker).Get(context.TODO(), tracker.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		unstructuredTracker, err := runtime.DefaultUnstructuredConverter.ToUnstructured(tracker)
		if err != nil {
			return err
		}

		u := unstructured.Unstructured{Object: unstructuredTracker}

		foundTracker, err = f.DynamicClient.Resource(gvr.ResourceTracker).Create(context.TODO(), &u, metav1.CreateOptions{})
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
		err := f.DynamicClient.Resource(gvr.ResourceTracker).Delete(context.TODO(), f.Spec.Tracker.Name, metav1.DeleteOptions{})

		if !k8serrors.IsNotFound(err) {
			return err
		}

		return nil
	})

	return nil
}
