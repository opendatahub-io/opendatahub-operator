package resources

import (
	"crypto/sha256"
	"fmt"

	"github.com/davecgh/go-spew/spew"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

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

func SetLabel(obj client.Object, k string, v string) {
	target := obj.GetLabels()
	if target == nil {
		target = make(map[string]string)
	}

	target[k] = v

	obj.SetLabels(target)
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

func SetAnnotation(obj client.Object, k string, v string) {
	target := obj.GetAnnotations()
	if target == nil {
		target = make(map[string]string)
	}

	target[k] = v

	obj.SetAnnotations(target)
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
