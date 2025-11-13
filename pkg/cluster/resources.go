package cluster

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apiextensions-apiserver/pkg/apihelpers"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// GetSingleton retrieves a singleton instance of a Kubernetes resource of type T.
//
// It ensures that only one instance exists and updates the provided object pointer
// with the retrieved data and:
//   - If no instances are found, it returns a "NotFound" error.
//   - If multiple instances are found, it returns an error indicating an unexpected
//     number of instances.
//   - A generic error in case of other failures
//
// Generic Parameters:
//   - T: A Kubernetes API resource that implements client.Object.
//     T **must be a pointer to a struct**, allowing the function to update its contents.
//
// Parameters:
//   - ctx: The context for the API request, allowing for cancellation and timeouts.
//   - cli: The Kubernetes client used to interact with the cluster.
//   - obj: A **pointer to a struct** that implements client.Object, which will be populated with the retrieved resource.
//
// Returns:
//   - nil if exactly one instance of the resource is found and successfully assigned to obj.
//   - An error if no instances or multiple instances are found, or if any failure occurs.
func GetSingleton[T client.Object](ctx context.Context, cli client.Client, obj T) error {
	if reflect.ValueOf(obj).IsNil() {
		return errors.New("obj must be a pointer")
	}

	objGVK, err := resources.GetGroupVersionKindForObject(cli.Scheme(), obj)
	if err != nil {
		return err
	}

	instances, err := ListGVK(ctx, cli, objGVK)
	if err != nil {
		return err
	}

	switch len(instances) {
	case 1:
		if err := cli.Scheme().Convert(&instances[0], obj, ctx); err != nil {
			return fmt.Errorf("failed to convert resource to %T: %w", obj, err)
		}
		return nil
	case 0:
		mapping, err := cli.RESTMapper().RESTMapping(objGVK.GroupKind(), objGVK.Version)
		if err != nil {
			return fmt.Errorf("failed to get REST mapping for %s: %w", objGVK, err)
		}

		return k8serr.NewNotFound(
			schema.GroupResource{
				Group:    objGVK.Group,
				Resource: mapping.Resource.Resource,
			},
			"",
		)
	default:
		return fmt.Errorf("failed to get a valid %s instance, expected to find 1 instance, found %d", objGVK, len(instances))
	}
}

// GetDSC retrieves the DataScienceCluster (DSC) instance from the Kubernetes cluster.
func GetDSC(ctx context.Context, cli client.Reader) (*dscv2.DataScienceCluster, error) {
	instances := dscv2.DataScienceClusterList{}
	if err := cli.List(ctx, &instances); err != nil {
		return nil, fmt.Errorf("failed to list resources of type %s: %w", gvk.DataScienceCluster, err)
	}

	switch len(instances.Items) {
	case 1:
		return &instances.Items[0], nil
	case 0:
		return nil, k8serr.NewNotFound(
			schema.GroupResource{
				Group:    gvk.DataScienceCluster.Group,
				Resource: "datascienceclusters",
			},
			"",
		)
	default:
		return nil, fmt.Errorf("failed to get a valid %s instance, expected to find 1 instance, found %d", gvk.DataScienceCluster, len(instances.Items))
	}
}

// GetDSCI retrieves the DSCInitialization (DSCI) instance from the Kubernetes cluster.
func GetDSCI(ctx context.Context, cli client.Client) (*dsciv2.DSCInitialization, error) {
	instances := dsciv2.DSCInitializationList{}
	if err := cli.List(ctx, &instances); err != nil {
		return nil, fmt.Errorf("failed to list resources of type %s: %w", gvk.DSCInitialization, err)
	}

	switch len(instances.Items) {
	case 1:
		return &instances.Items[0], nil
	case 0:
		return nil, k8serr.NewNotFound(
			schema.GroupResource{
				Group:    gvk.DSCInitialization.Group,
				Resource: "dscinitializations",
			},
			"",
		)
	default:
		return nil, fmt.Errorf("failed to get a valid %s instance, expected to find 1 instance, found %d", gvk.DSCInitialization, len(instances.Items))
	}
}

// ApplicationNamespace returns the applications namespace from DSCInitialization.
// Returns an error if DSCI is not found or cannot be retrieved.
func ApplicationNamespace(ctx context.Context, cli client.Client) (string, error) {
	dsci, err := GetDSCI(ctx, cli)
	if err != nil {
		if k8serr.IsNotFound(err) {
			return "", fmt.Errorf("ApplicationsNamespace not available, DSCI not found: %w", err)
		}
		return "", fmt.Errorf("failed to get DSCInitialization: %w", err)
	}
	return dsci.Spec.ApplicationsNamespace, nil
}

// MonitoringNamespace returns the monitoring namespace from DSCInitialization.
// Returns an error if DSCI is not found or cannot be retrieved.
func MonitoringNamespace(ctx context.Context, cli client.Client) (string, error) {
	dsci, err := GetDSCI(ctx, cli)
	if err != nil {
		if k8serr.IsNotFound(err) {
			return "", fmt.Errorf("MonitoringNamespace not available, DSCI not found: %w", err)
		}
		return "", fmt.Errorf("failed to get DSCInitialization: %w", err)
	}
	return dsci.Spec.Monitoring.Namespace, nil
}

// GetHardwareProfile retrieves a specific HardwareProfile instance by name and namespace.
func GetHardwareProfile(ctx context.Context, cli client.Client, name, namespace string) (*infrav1.HardwareProfile, error) {
	hwProfile := &infrav1.HardwareProfile{}
	err := cli.Get(ctx, client.ObjectKey{
		Name:      name,
		Namespace: namespace,
	}, hwProfile)
	if err != nil {
		return nil, err
	}
	return hwProfile, nil
}

// CreateHardwareProfile creates a HardwareProfile resource in the cluster.
// If the resource already exists, it returns nil (idempotent operation).
//
// Parameters:
//   - ctx: The context for the request
//   - cli: The Kubernetes client used to create the resource
//   - hwp: The HardwareProfile to create
//
// Returns:
//   - error: An error if the creation fails for reasons other than "already exists"
func CreateHardwareProfile(ctx context.Context, cli client.Client, hwp *infrav1.HardwareProfile) error {
	if err := cli.Create(ctx, hwp); err != nil {
		if !k8serr.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create HardwareProfile '%s/%s': %w", hwp.Namespace, hwp.Name, err)
		}
	}
	return nil
}

// UpdatePodSecurityRolebinding update default rolebinding which is created in applications namespace by manifests
// being used by different components and SRE monitoring.
func UpdatePodSecurityRolebinding(ctx context.Context, cli client.Client, namespace string, serviceAccountsList ...string) error {
	foundRoleBinding := &rbacv1.RoleBinding{}
	if err := cli.Get(ctx, client.ObjectKey{Name: namespace, Namespace: namespace}, foundRoleBinding); err != nil {
		return fmt.Errorf("error to get rolebinding %s from namespace %s: %w", namespace, namespace, err)
	}

	for _, sa := range serviceAccountsList {
		// Append serviceAccount if not added already
		if !SubjectExistInRoleBinding(foundRoleBinding.Subjects, sa, namespace) {
			foundRoleBinding.Subjects = append(foundRoleBinding.Subjects, rbacv1.Subject{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      sa,
				Namespace: namespace,
			})
		}
	}

	if err := cli.Update(ctx, foundRoleBinding); err != nil {
		return fmt.Errorf("error update rolebinding %s with serviceaccount: %w", namespace, err)
	}

	return nil
}

// SubjectExistInRoleBinding return whether RoleBinding matching service account and namespace exists or not.
func SubjectExistInRoleBinding(subjectList []rbacv1.Subject, serviceAccountName, namespace string) bool {
	for _, subject := range subjectList {
		if subject.Name == serviceAccountName && subject.Namespace == namespace {
			return true
		}
	}

	return false
}

// CreateOrUpdateConfigMap creates a new configmap or updates an existing one.
// If the configmap already exists, it will be updated with the merged Data and MetaOptions, if any.
// ConfigMap.ObjectMeta.Name and ConfigMap.ObjectMeta.Namespace are both required, it returns an error otherwise.
func CreateOrUpdateConfigMap(ctx context.Context, c client.Client, desiredCfgMap *corev1.ConfigMap, metaOptions ...MetaOptions) error {
	if applyErr := ApplyMetaOptions(desiredCfgMap, metaOptions...); applyErr != nil {
		return applyErr
	}

	if desiredCfgMap.GetName() == "" || desiredCfgMap.GetNamespace() == "" {
		return errors.New("configmap name and namespace must be set")
	}

	// Explicitly setting the TypeMeta is required to use resources.Apply()
	// Otherwise will return error stating that the unstructured object has no kind
	desiredCfgMap.TypeMeta = metav1.TypeMeta{
		APIVersion: gvk.ConfigMap.Version,
		Kind:       gvk.ConfigMap.Kind,
	}

	opts := []client.PatchOption{
		client.ForceOwnership,
		client.FieldOwner(resources.PlatformFieldOwner),
	}
	err := resources.Apply(ctx, c, desiredCfgMap, opts...)
	if err != nil && !k8serr.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// CreateNamespace creates a namespace and apply metadata.
// If a namespace already exists, the operation has no effect on it.
func CreateNamespace(ctx context.Context, cli client.Client, namespace string, metaOptions ...MetaOptions) (*corev1.Namespace, error) {
	desiredNamespace := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gvk.Namespace.Version,
			Kind:       gvk.Namespace.Kind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}

	if err := ApplyMetaOptions(desiredNamespace, metaOptions...); err != nil {
		return nil, err
	}

	opts := []client.PatchOption{
		client.ForceOwnership,
		client.FieldOwner(resources.PlatformFieldOwner),
	}
	err := resources.Apply(ctx, cli, desiredNamespace, opts...)
	if err != nil && !k8serr.IsAlreadyExists(err) {
		return nil, err
	}

	return desiredNamespace, nil
}

// ExecuteOnAllNamespaces executes the passed function for all namespaces in the cluster retrieved in batches.
func ExecuteOnAllNamespaces(ctx context.Context, cli client.Client, processFunc func(*corev1.Namespace) error) error {
	namespaces := &corev1.NamespaceList{}
	paginateListOption := &client.ListOptions{
		Limit: 500,
	}

	for { // loop over all paged results
		if err := cli.List(ctx, namespaces, paginateListOption); err != nil {
			return err
		}
		for i := range namespaces.Items {
			ns := &namespaces.Items[i]
			if err := processFunc(ns); err != nil {
				return err
			}
		}
		if paginateListOption.Continue = namespaces.GetContinue(); namespaces.GetContinue() == "" {
			break
		}
	}
	return nil
}

func CreateWithRetry(ctx context.Context, cli client.Client, obj client.Object) error {
	log := logf.FromContext(ctx)
	backoff := wait.Backoff{
		Duration: 2 * time.Second,
		Factor:   2.0,
		Steps:    5,
	}
	// 1 minute timeout
	return wait.ExponentialBackoffWithContext(ctx, backoff, func(ctx context.Context) (bool, error) {
		// Create can return:
		// If webhook enabled:
		//   - no error (err == nil)
		//   - 500 InternalError likely if webhook is not available (yet)
		//   - 403 Forbidden if webhook blocks creation (check of existence)
		//   - some problem (real error)
		// else, if webhook disabled:
		//   - no error (err == nil)
		//   - 409 AlreadyExists if object exists
		//   - some problem (real error)
		errCreate := cli.Create(ctx, obj)
		if errCreate == nil {
			return true, nil
		}

		// check existence, success case for the function, covers 409 and 403 (or newly created)
		errGet := cli.Get(ctx, client.ObjectKeyFromObject(obj), obj)
		if errGet == nil {
			return true, nil
		}

		// retry if 500, assume webhook is not available
		if k8serr.IsInternalError(errCreate) {
			log.Info("Error creating object, retrying...", "reason", errCreate)
			return false, nil
		}

		// some other error
		return false, errCreate
	})
}

func GetCRD(ctx context.Context, cli client.Client, name string) (apiextensionsv1.CustomResourceDefinition, error) {
	obj := apiextensionsv1.CustomResourceDefinition{}
	err := cli.Get(ctx, client.ObjectKey{Name: name}, &obj)
	if err != nil {
		return obj, err
	}

	return obj, nil
}

func HasCRD(ctx context.Context, cli client.Client, gvk schema.GroupVersionKind) (bool, error) {
	return HasCRDWithVersion(ctx, cli, gvk.GroupKind(), gvk.Version)
}

// HasCRDWithVersion checks if a CustomResourceDefinition (CRD) exists with the specified version.
// It verifies the CRD's existence, ensures that the version is stored, and checks if the CRD is under deletion.
//
// Parameters:
//   - ctx: The context for the request.
//   - cli: A controller-runtime client to interact with the Kubernetes API.
//   - gk: The GroupKind of the CRD to look up.
//   - version: The specific version to check for within the CRD.
//
// Returns:
//   - (true, nil) if the CRD with the specified version exists and is not terminating.
//   - (false, nil) if the CRD does not exist, does not store the requested version, or is terminating.
//   - (false, error) if there was an error fetching the CRD.
func HasCRDWithVersion(ctx context.Context, cli client.Client, gk schema.GroupKind, version string) (bool, error) {
	m, err := cli.RESTMapper().RESTMapping(gk, version)
	if err != nil {
		if meta.IsNoMatchError(err) {
			return false, nil
		}

		return false, err
	}

	crd, err := GetCRD(ctx, cli, m.Resource.GroupResource().String())
	switch {
	case err != nil:
		return false, client.IgnoreNotFound(err)
	case apihelpers.IsCRDConditionTrue(&crd, apiextensionsv1.Terminating):
		return false, nil
	case !slices.Contains(crd.Status.StoredVersions, version):
		return false, nil
	default:
		return true, nil
	}
}

func ListGVK(ctx context.Context, cli client.Client, gvk schema.GroupVersionKind, listOptions ...client.ListOption) ([]unstructured.Unstructured, error) {
	resources := unstructured.UnstructuredList{}
	resources.SetAPIVersion(gvk.GroupVersion().String())
	resources.SetKind(gvk.Kind)

	if err := cli.List(ctx, &resources, listOptions...); err != nil {
		return nil, fmt.Errorf("failed to list resources of type %s: %w", gvk, err)
	}
	return resources.Items, nil
}
