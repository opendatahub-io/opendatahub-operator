package feature

import (
	"context"
	"github.com/hashicorp/go-multierror"
	"github.com/opendatahub-io/opendatahub-operator/apis/ossm.plugins.kubeflow.org/v1alpha1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"net/url"
	"regexp"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"
	"strings"
)

var log = ctrlLog.Log.WithName("ossm-features")

type Feature struct {
	Name string
	Spec *Spec

	clientset     *kubernetes.Clientset
	dynamicClient dynamic.Interface
	client        client.Client

	manifests      []manifest
	cleanups       []action
	resources      []action
	preconditions  []action
	postconditions []action
	loaders        []action
}

// action is a func type which can be used for different purposes while having access to Feature struct
type action func(feature *Feature) error

func (f *Feature) Apply() error {
	// Verify all precondition and collect errors
	var multiErr *multierror.Error
	for _, precondition := range f.preconditions {
		multiErr = multierror.Append(multiErr, precondition(f))
	}

	if multiErr.ErrorOrNil() != nil {
		return multiErr.ErrorOrNil()
	}

	// Load necessary data
	for _, loader := range f.loaders {
		multiErr = multierror.Append(multiErr, loader(f))
	}
	if multiErr.ErrorOrNil() != nil {
		return multiErr.ErrorOrNil()
	}

	// create or update resources
	for _, resource := range f.resources {
		if err := resource(f); err != nil {
			return err
		}
	}

	// Process and apply manifests
	for _, m := range f.manifests {
		if err := m.processTemplate(f.Spec); err != nil {
			return errors.WithStack(err)
		}

		log.Info("applying manifest", "path", m.targetPath())
	}

	if err := f.applyManifests(); err != nil {
		return err
	}

	// TODO postconditions

	return nil
}

func (f *Feature) Cleanup() error {
	var cleanupErrors *multierror.Error
	for _, cleanupFunc := range f.cleanups {
		cleanupErrors = multierror.Append(cleanupErrors, cleanupFunc(f))
	}

	return cleanupErrors.ErrorOrNil()
}

func (f *Feature) applyManifests() error {
	var applyErrors *multierror.Error
	for _, m := range f.manifests {
		err := f.apply(m)
		applyErrors = multierror.Append(applyErrors, err)
	}

	return applyErrors.ErrorOrNil()
}

func (f *Feature) createConfigMap(cfgMapName string, data map[string]string) error {

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

	configMaps := f.clientset.CoreV1().ConfigMaps(configMap.Namespace)

	_, err := configMaps.Get(context.TODO(), configMap.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
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

func (f *Feature) addCleanup(cleanupFuncs ...action) {
	f.cleanups = append(f.cleanups, cleanupFuncs...)
}

type apply func(filename string) error

func (f *Feature) apply(m manifest) error {
	var applier apply
	targetPath := m.targetPath()

	if m.patch {
		applier = func(filename string) error {
			log.Info("patching using manifest", "name", m.name, "path", targetPath)

			return f.patchResourceFromFile(filename)
		}
	} else {
		applier = func(filename string) error {
			log.Info("applying manifest", "name", m.name, "path", targetPath)

			return f.createResourceFromFile(filename)
		}
	}

	if err := applier(targetPath); err != nil {
		log.Error(err, "failed to create resource", "name", m.name, "path", targetPath)

		return err
	}

	return nil
}

func (f *Feature) OwnerReference() metav1.OwnerReference {
	return f.Spec.Tracker.ToOwnerReference()
}

// createResourceTracker instantiates OssmResourceTracker for given a Feature. All resources created when applying
// it will have this object attached as OwnerReference. It's a cluster-scoped resource.
// Once created, there's a cleanup hook added which will be invoked on deletion.
func (f *Feature) createResourceTracker() error {
	tracker := &v1alpha1.OssmResourceTracker{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "ossm.plugins.kubeflow.org/v1alpha1",
			Kind:       "OssmResourceTracker",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: f.Spec.AppNamespace + "-" + convertToRFC1123Subdomain(f.Name),
		},
	}

	gvr := schema.GroupVersionResource{
		Group:    "ossm.plugins.kubeflow.org",
		Version:  "v1alpha1",
		Resource: "ossmresourcetrackers",
	}

	foundTracker, err := f.dynamicClient.Resource(gvr).Get(context.Background(), tracker.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		unstructuredTracker, err := runtime.DefaultUnstructuredConverter.ToUnstructured(tracker)
		if err != nil {
			return err
		}

		u := unstructured.Unstructured{Object: unstructuredTracker}

		foundTracker, err = f.dynamicClient.Resource(gvr).Create(context.Background(), &u, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	f.Spec.Tracker = &v1alpha1.OssmResourceTracker{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(foundTracker.Object, f.Spec.Tracker); err != nil {
		return err
	}

	// Register its own cleanup
	f.addCleanup(func(feature *Feature) error {
		if err := f.dynamicClient.Resource(gvr).Delete(context.Background(), f.Spec.Tracker.Name, metav1.DeleteOptions{}); err != nil && !k8serrors.IsNotFound(err) {
			return err
		}

		return nil
	})

	return nil
}

func convertToRFC1123Subdomain(input string) string {
	escaped := url.PathEscape(input)

	// Define a regular expression to match characters that need to be replaced
	regex := regexp.MustCompile(`[^A-Za-z0-9.\-_]+`)

	// Replace non-alphanumeric characters with a hyphen
	replaced := regex.ReplaceAllString(escaped, "-")

	// Convert the result to lowercase
	return strings.ToLower(replaced)
}
