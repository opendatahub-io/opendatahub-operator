package resources

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/davecgh/go-spew/spew"
	"github.com/hashicorp/go-multierror"
	routev1 "github.com/openshift/api/route/v1"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const PlatformFieldOwner = "platform.opendatahub.io"

// ResourceSpec defines a specification for identifying and filtering Kubernetes resources
// based on their GroupVersionKind, namespace, and field values.
type ResourceSpec struct {
	Gvk       schema.GroupVersionKind
	Namespace string
	// FieldPath specifies the path to the field for filtering, like ["metadata", "name"]
	FieldPath []string
	// FilterValues contains the values to match against the field - if the field value
	// matches any of these values, the resource will be processed (e.g., deleted)
	FilterValues []string
}

func ToUnstructured(obj any) (*unstructured.Unstructured, error) {
	data, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil, fmt.Errorf("unable to convert object %T to unstructured: %w", obj, err)
	}

	u := unstructured.Unstructured{
		Object: data,
	}

	return &u, nil
}

func ObjectToUnstructured(s *runtime.Scheme, obj client.Object) (*unstructured.Unstructured, error) {
	// Ensure that the object has a GroupVersionKind set
	if err := EnsureGroupVersionKind(s, obj); err != nil {
		return nil, fmt.Errorf("failed to ensure GroupVersionKind: %w", err)
	}

	// Now, convert the object to unstructured
	u, err := ToUnstructured(obj)
	if err != nil {
		return nil, err
	}

	return u, nil
}

func ObjectFromUnstructured(s *runtime.Scheme, obj *unstructured.Unstructured, intoObj client.Object) error {
	if obj == nil {
		return errors.New("nil object")
	}

	// Convert the unstructured object to the typed object
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, intoObj)
	if err != nil {
		return fmt.Errorf("unable to convert unstructured object to %T: %w", intoObj, err)
	}

	// Ensure that the GroupVersionKind is correctly set on the target object
	err = EnsureGroupVersionKind(s, intoObj)
	if err != nil {
		return fmt.Errorf("unable to ensure GroupVersionKind: %w", err)
	}

	// Validate that the GroupVersionKind is known in the scheme
	gvk := intoObj.GetObjectKind().GroupVersionKind()
	if _, err := s.New(gvk); err != nil {
		return fmt.Errorf("unable to create object for GVK %s: %w", gvk, err)
	}

	return nil
}

func Decode(decoder runtime.Decoder, content []byte) ([]unstructured.Unstructured, error) {
	results := make([]unstructured.Unstructured, 0)

	r := bytes.NewReader(content)
	yd := yaml.NewDecoder(r)

	for {
		var out map[string]interface{}

		err := yd.Decode(&out)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			return nil, fmt.Errorf("unable to decode resource: %w", err)
		}

		if len(out) == 0 {
			continue
		}

		if out["Kind"] == "" {
			continue
		}

		encoded, err := yaml.Marshal(out)
		if err != nil {
			return nil, fmt.Errorf("unable to marshal resource: %w", err)
		}

		var obj unstructured.Unstructured

		if _, _, err = decoder.Decode(encoded, nil, &obj); err != nil {
			if runtime.IsMissingKind(err) {
				continue
			}

			return nil, fmt.Errorf("unable to decode resource: %w", err)
		}

		results = append(results, obj)
	}

	return results, nil
}

func GvkToUnstructured(gvk schema.GroupVersionKind) *unstructured.Unstructured {
	u := unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)

	return &u
}

func IngressHost(r routev1.Route) string {
	if len(r.Status.Ingress) != 1 {
		return ""
	}

	in := r.Status.Ingress[0]

	for i := range in.Conditions {
		if in.Conditions[i].Type == routev1.RouteAdmitted && in.Conditions[i].Status == corev1.ConditionTrue {
			return in.Host
		}
	}

	return ""
}

func HasLabel(obj client.Object, k string, values ...string) bool {
	if obj == nil {
		return false
	}

	target := obj.GetLabels()
	if target == nil {
		return false
	}

	val, found := target[k]
	if !found {
		return false
	}

	return slices.Contains(values, val)
}

func SetLabels(obj client.Object, values map[string]string) {
	target := obj.GetLabels()
	if target == nil {
		target = make(map[string]string)
	}

	for k, v := range values {
		target[k] = v
	}

	obj.SetLabels(target)
}

func SetLabel(obj client.Object, k string, v string) string {
	target := obj.GetLabels()
	if target == nil {
		target = make(map[string]string)
	}

	old := target[k]
	target[k] = v

	obj.SetLabels(target)

	return old
}

func RemoveLabel(obj client.Object, k string) {
	target := obj.GetLabels()
	if target == nil {
		return
	}

	delete(target, k)

	obj.SetLabels(target)
}

func GetLabel(obj client.Object, k string) string {
	target := obj.GetLabels()
	if target == nil {
		return ""
	}

	return target[k]
}

func HasAnnotation(obj client.Object, k string, values ...string) bool {
	if obj == nil {
		return false
	}

	target := obj.GetAnnotations()
	if target == nil {
		return false
	}

	val, found := target[k]
	if !found {
		return false
	}

	return slices.Contains(values, val)
}

func SetAnnotations(obj client.Object, values map[string]string) {
	target := obj.GetAnnotations()
	if target == nil {
		target = make(map[string]string)
	}

	for k, v := range values {
		target[k] = v
	}

	obj.SetAnnotations(target)
}

func SetAnnotation(obj client.Object, k string, v string) string {
	target := obj.GetAnnotations()
	if target == nil {
		target = make(map[string]string)
	}

	old := target[k]
	target[k] = v

	obj.SetAnnotations(target)

	return old
}

func RemoveAnnotation(obj client.Object, k string) {
	target := obj.GetAnnotations()
	if target == nil {
		return
	}

	delete(target, k)

	obj.SetAnnotations(target)
}

func GetAnnotation(obj client.Object, k string) string {
	target := obj.GetAnnotations()
	if target == nil {
		return ""
	}

	return target[k]
}

// Hash generates an SHA-256 hash of an unstructured Kubernetes object, omitting
// specific fields that are typically irrelevant for hash comparison such as
// "creationTimestamp", "deletionTimestamp", "managedFields", "ownerReferences",
// "uid", "resourceVersion", and "status". It returns the computed hash as a byte
// slice or an error if the hashing process fails.
func Hash(in *unstructured.Unstructured) ([]byte, error) {
	obj := in.DeepCopy()
	unstructured.RemoveNestedField(obj.Object, "metadata", "uid")
	unstructured.RemoveNestedField(obj.Object, "metadata", "resourceVersion")
	unstructured.RemoveNestedField(obj.Object, "metadata", "deletionTimestamp")
	unstructured.RemoveNestedField(obj.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(obj.Object, "metadata", "ownerReferences")
	unstructured.RemoveNestedField(obj.Object, "status")

	printer := spew.ConfigState{
		Indent:         " ",
		SortKeys:       true,
		DisableMethods: true,
		SpewKeys:       true,
	}

	hasher := sha256.New()

	if _, err := printer.Fprintf(hasher, "%#v", obj); err != nil {
		return nil, fmt.Errorf("failed to calculate hash: %w", err)
	}

	return hasher.Sum(nil), nil
}

// StripServerMetadata removes server-managed metadata fields from a resource,
// returning a clean copy suitable for operations like creation, comparison, or backup.
func StripServerMetadata(obj *unstructured.Unstructured) *unstructured.Unstructured {
	if obj == nil {
		return nil
	}
	clean := obj.DeepCopy()

	// Remove server-managed metadata fields (same pattern as Hash function)
	unstructured.RemoveNestedField(clean.Object, "metadata", "uid")
	unstructured.RemoveNestedField(clean.Object, "metadata", "resourceVersion")
	unstructured.RemoveNestedField(clean.Object, "metadata", "generation")
	unstructured.RemoveNestedField(clean.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(clean.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(clean.Object, "metadata", "deletionTimestamp")
	unstructured.RemoveNestedField(clean.Object, "metadata", "ownerReferences")
	unstructured.RemoveNestedField(clean.Object, "status")

	return clean
}

func EncodeToString(in []byte) string {
	return "v" + base64.RawURLEncoding.EncodeToString(in)
}

func KindForObject(scheme *runtime.Scheme, obj runtime.Object) (string, error) {
	if obj.GetObjectKind().GroupVersionKind().Kind != "" {
		return obj.GetObjectKind().GroupVersionKind().Kind, nil
	}

	gvk, err := apiutil.GVKForObject(obj, scheme)
	if err != nil {
		return "", fmt.Errorf("failed to get GVK: %w", err)
	}

	return gvk.Kind, nil
}

func GetGroupVersionKindForObject(s *runtime.Scheme, obj runtime.Object) (schema.GroupVersionKind, error) {
	if obj == nil {
		return schema.GroupVersionKind{}, errors.New("nil object")
	}

	if obj.GetObjectKind().GroupVersionKind().Version != "" && obj.GetObjectKind().GroupVersionKind().Kind != "" {
		return obj.GetObjectKind().GroupVersionKind(), nil
	}

	gvk, err := apiutil.GVKForObject(obj, s)
	if err != nil {
		return schema.GroupVersionKind{}, fmt.Errorf("failed to get GVK: %w", err)
	}

	return gvk, nil
}

func EnsureGroupVersionKind(s *runtime.Scheme, obj client.Object) error {
	gvk, err := GetGroupVersionKindForObject(s, obj)
	if err != nil {
		return err
	}

	obj.GetObjectKind().SetGroupVersionKind(gvk)

	return nil
}

func NamespacedNameFromObject(obj client.Object) types.NamespacedName {
	return types.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}
}

func FormatNamespacedName(nn types.NamespacedName) string {
	if nn.Namespace == "" {
		return nn.Name
	}
	return nn.String()
}

func FormatUnstructuredName(obj *unstructured.Unstructured) string {
	if obj.GetNamespace() == "" {
		return obj.GetName()
	}
	return obj.GetNamespace() + string(types.Separator) + obj.GetName()
}

func FormatObjectReference(u *unstructured.Unstructured) string {
	gvk := u.GroupVersionKind().String()
	name := u.GetName()
	ns := u.GetNamespace()
	if ns != "" {
		return gvk + " " + ns + "/" + name
	}
	return gvk + " " + name
}

// RemoveOwnerReferences removes all owner references from a Kubernetes object that match the provided predicate.
//
// This function iterates through the OwnerReferences of the given object, filters out those that satisfy
// the predicate, and updates the object in the cluster using the provided client.
//
// Parameters:
//   - ctx: The context for the request, which can carry deadlines, cancellation signals, and other request-scoped values.
//   - cli: A controller-runtime client used to update the Kubernetes object.
//   - obj: The Kubernetes object whose OwnerReferences are to be filtered. It must implement client.Object.
//   - predicate: A function that takes an OwnerReference and returns true if the reference should be removed.
//
// Returns:
//   - An error if the update operation fails, otherwise nil.
func RemoveOwnerReferences(
	ctx context.Context,
	cli client.Client,
	obj client.Object,
	predicate func(reference metav1.OwnerReference) bool,
) error {
	oldRefs := obj.GetOwnerReferences()
	if len(oldRefs) == 0 {
		return nil
	}

	newRefs := oldRefs[:0]
	for _, ref := range oldRefs {
		if !predicate(ref) {
			newRefs = append(newRefs, ref)
		}
	}

	if len(newRefs) == len(oldRefs) {
		return nil
	}

	obj.SetOwnerReferences(newRefs)

	// Update the object in the cluster
	if err := cli.Update(ctx, obj); err != nil {
		return fmt.Errorf(
			"failed to remove owner references from object %s/%s with gvk %s: %w",
			obj.GetNamespace(),
			obj.GetName(),
			obj.GetObjectKind().GroupVersionKind(),
			err,
		)
	}

	return nil
}

// IsOwnedByType checks if the given object (obj) is owned by an entity of the specified GroupVersionKind.
// It iterates through the object's owner references to determine ownership.
//
// Parameters:
// - obj: The Kubernetes object to check ownership for.
// - ownerGVK: The GroupVersionKind (GVK) of the expected owner.
//
// Returns:
// - A boolean indicating whether the object is owned by an entity of the specified GVK.
// - An error if any issue occurs while parsing the owner's API version.
func IsOwnedByType(obj client.Object, ownerGVK schema.GroupVersionKind) (bool, error) {
	for _, ref := range obj.GetOwnerReferences() {
		av, err := schema.ParseGroupVersion(ref.APIVersion)
		if err != nil {
			return false, err
		}

		if av.Group == ownerGVK.Group && av.Version == ownerGVK.Version && ref.Kind == ownerGVK.Kind {
			return true, nil
		}
	}

	return false, nil
}

func GvkToPartial(gvk schema.GroupVersionKind) *metav1.PartialObjectMetadata {
	return &metav1.PartialObjectMetadata{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gvk.GroupVersion().String(),
			Kind:       gvk.Kind,
		},
	}
}

// Apply patches an object using server-side apply.
//
// This function converts the input object to an unstructured type, removes fields that
// are not required for patching (i.e. managedFields, resourceVersion, and status), applies
// the patch, and updates the input object with the patched result.
//
// The function handles the case where the resource doesn't exist (NotFound error) by treating
// it as a success condition and returning nil.
//
// Parameters:
//   - ctx: The context for the client operation
//   - cli: The Kubernetes client interface used to perform the patch operation
//   - in: The Kubernetes object to be applied
//   - opts: Optional client patch options
//
// Returns:
//   - error: nil on success, or an error with context if the operation fails
func Apply(ctx context.Context, cli client.Client, in client.Object, opts ...client.PatchOption) error {
	err := EnsureGroupVersionKind(cli.Scheme(), in)
	if err != nil {
		return fmt.Errorf("failed to ensure GVK: %w", err)
	}

	u, err := ToUnstructured(in)
	if err != nil {
		return fmt.Errorf("failed to convert resource to unstructured: %w", err)
	}

	// safe copy
	u = u.DeepCopy()

	// remove not required fields
	unstructured.RemoveNestedField(u.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(u.Object, "metadata", "resourceVersion")
	unstructured.RemoveNestedField(u.Object, "status")

	err = cli.Patch(ctx, u, client.Apply, opts...)
	if err != nil {
		// Include GVK and namespace/name for debugging context without logging sensitive object data
		objRef := FormatObjectReference(u)
		return fmt.Errorf("unable to patch %s: %w", objRef, err)
	}

	// Write back the modified object so callers can access the patched object.
	err = cli.Scheme().Convert(u, in, ctx)
	if err != nil {
		return fmt.Errorf("failed to write modified object: %w", err)
	}

	return nil
}

// ApplyStatus patches the status subresource of a Kubernetes object using server-side apply.
//
// This function converts the input object to an unstructured type, removes fields that are
// not required for patching (i.e. managedFields and resourceVersion), applies the patch to
// the status subresource, and updates the input object with the patched result.
//
// The function handles the case where the resource doesn't exist (NotFound error) by treating
// it as a success condition and returning nil.
//
// Parameters:
//   - ctx: The context for the client operation
//   - cli: The Kubernetes client interface used to perform the patch operation
//   - in: The Kubernetes object whose status should be applied
//   - opts: Optional client subresource patch options
//
// Returns:
//   - error: nil on success, or an error with context if the operation fails
func ApplyStatus(ctx context.Context, cli client.Client, in client.Object, opts ...client.SubResourcePatchOption) error {
	err := EnsureGroupVersionKind(cli.Scheme(), in)
	if err != nil {
		return fmt.Errorf("failed to ensure GVK: %w", err)
	}

	u, err := ToUnstructured(in)
	if err != nil {
		return fmt.Errorf("failed to convert resource to unstructured: %w", err)
	}

	// safe copy
	u = u.DeepCopy()

	// remove not required fields
	unstructured.RemoveNestedField(u.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(u.Object, "metadata", "resourceVersion")

	err = cli.Status().Patch(ctx, u, client.Apply, opts...)
	switch {
	case k8serr.IsNotFound(err): // Cannot be removed like in Apply func because reconciler_finalizer_test.go would then throw an error, needs extensive test rewrite
		return nil
	case err != nil:
		// Include GVK and namespace/name for debugging context without logging sensitive object data
		objRef := FormatObjectReference(u)
		return fmt.Errorf("unable to patch %s status: %w", objRef, err)
	}

	// Write back the modified object so callers can access the patched object.
	err = cli.Scheme().Convert(u, in, ctx)
	if err != nil {
		return fmt.Errorf("failed to write modified object: %w", err)
	}

	return nil
}

// ListAvailableAPIResources retrieves the preferred API resource lists available on
// the Kubernetes server using the provided discovery client.
//
// This function calls ServerPreferredResources to get a prioritized list of
// APIResourceList, which describes the API groups, versions, and resources the
// server supports.
//
// It gracefully handles partial discovery failures (e.g. due to aggregated APIs like
// Knative or custom metrics APIs that may require special permissions). In such cases,
// it still returns the successfully discovered resource lists.
//
// Parameters:
//   - cli: A discovery.DiscoveryInterface used to interact with the Kubernetes API server.
//
// Returns:
//   - []*metav1.APIResourceList: A list of resource groups and versions supported by
//     the server.
//   - error: Non-nil only if a non-group-discovery-related error occurs during discovery.
func ListAvailableAPIResources(
	cli discovery.DiscoveryInterface,
) ([]*metav1.APIResourceList, error) {
	items, err := cli.ServerPreferredResources()

	// Swallow group discovery errors, e.g., Knative serving exposes an
	// aggregated API for custom.metrics.k8s.io that requires special
	// authentication scheme while discovering preferred resources.
	if err != nil && !discovery.IsGroupDiscoveryFailedError(err) {
		return nil, fmt.Errorf("failure retrieving supported resources: %w", err)
	}

	return items, nil
}

// DeleteResources iterates through a list of ResourceSpec and deletes matching Kubernetes resources.
// It collects all errors encountered during deletion and returns them as a multierror.
//
// Parameters:
//   - ctx: The context for the operation
//   - c: The Kubernetes client used to list and delete resources
//   - resources: A slice of ResourceSpec defining which resources to delete
//
// Returns:
//   - error: A multierror containing all errors encountered, or nil if all deletions succeeded
func DeleteResources(ctx context.Context, c client.Client, resources []ResourceSpec) error {
	var errors *multierror.Error

	for _, res := range resources {
		err := DeleteOneResource(ctx, c, res)
		errors = multierror.Append(errors, err)
	}

	return errors.ErrorOrNil()
}

// DeleteOneResource deletes all Kubernetes resources matching the given ResourceSpec.
// It lists resources of the specified GVK in the given namespace, filters them based on
// the field path and values, and deletes matching resources.
//
// Parameters:
//   - ctx: The context for the operation
//   - c: The Kubernetes client used to list and delete resources
//   - res: The ResourceSpec defining which resources to delete
//
// Returns:
//   - error: An error if the operation fails, or nil on success
func DeleteOneResource(ctx context.Context, c client.Client, res ResourceSpec) error {
	log := logf.FromContext(ctx)
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(res.Gvk)

	err := c.List(ctx, list, client.InNamespace(res.Namespace))
	if err != nil {
		if meta.IsNoMatchError(err) {
			log.Info("CRD not found, will not delete", "gvk", res.Gvk.String())
			return nil
		}
		return fmt.Errorf("failed to list %s: %w", res.Gvk.Kind, err)
	}

	for _, item := range list.Items {
		v, ok, err := unstructured.NestedString(item.Object, res.FieldPath...)
		if err != nil {
			return fmt.Errorf("failed to get field %v for %s %s/%s: %w", res.FieldPath, res.Gvk.Kind, res.Namespace, item.GetName(), err)
		}

		if !ok {
			return fmt.Errorf("nonexistent field path: %v", res.FieldPath)
		}

		for _, targetValue := range res.FilterValues {
			if v == targetValue {
				err = c.Delete(ctx, &item)
				if err != nil {
					return fmt.Errorf("failed to delete %s %s/%s: %w", res.Gvk.Kind, res.Namespace, item.GetName(), err)
				}
				log.Info("Deleted object", "name", item.GetName(), "gvk", res.Gvk.String(), "namespace", res.Namespace)
			}
		}
	}

	return nil
}

// UnsetOwnerReferences removes all owner references from a Kubernetes object and updates it.
// This is useful when you need to orphan a resource from its owner.
//
// Parameters:
//   - ctx: The context for the operation
//   - cli: The Kubernetes client used to update the resource
//   - instanceName: The name of the instance (used for error reporting)
//   - odhObject: The unstructured object whose owner references should be removed
//
// Returns:
//   - error: An error if the update operation fails, or nil if there are no owner references or update succeeds
func UnsetOwnerReferences(ctx context.Context, cli client.Client, instanceName string, odhObject *unstructured.Unstructured) error {
	if odhObject.GetOwnerReferences() != nil {
		// set to nil as updates
		odhObject.SetOwnerReferences(nil)
		if err := cli.Update(ctx, odhObject); err != nil {
			return fmt.Errorf("error unset ownerreference for CR %s : %w", instanceName, err)
		}
	}
	return nil
}
