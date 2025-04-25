package resources

import (
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type UnstructuredList []unstructured.Unstructured

func (l UnstructuredList) Clone() []unstructured.Unstructured {
	if len(l) == 0 {
		return nil
	}

	result := make([]unstructured.Unstructured, len(l))

	for i := range l {
		result[i] = *l[i].DeepCopy()
	}

	return result
}

// Resource represents a Kubernetes API resource type with convenient methods
// for accessing common properties and relationships. It wraps the standard
// meta.RESTMapping struct to provide a more intuitive interface.
type Resource struct {
	meta.RESTMapping
}

// GroupVersionResource returns the schema.GroupVersionResource associated with this Resource.
func (r Resource) GroupVersionResource() schema.GroupVersionResource {
	return r.Resource
}

// GroupVersionKind returns the schema.GroupVersionKind associated with this Resource.
func (r Resource) GroupVersionKind() schema.GroupVersionKind {
	return r.RESTMapping.GroupVersionKind
}

// String returns a string representation of this Resource that includes both
// the GroupVersionResource and GroupVersionKind for better debugging.
func (r Resource) String() string {
	gv := r.Resource.Version

	if len(r.Resource.Group) > 0 {
		gv = r.Resource.Group + "/" + r.Resource.Version
	}

	return strings.Join(
		[]string{
			gv, "Resource=", r.Resource.Resource, "Kind=", r.RESTMapping.GroupVersionKind.Kind,
		},
		" ",
	)
}

// IsNamespaced returns true if this Resource is namespaced, false otherwise.
// Namespaced resources are those that exist within a Kubernetes namespace.
func (r Resource) IsNamespaced() bool {
	// The Scope field may be nil if the RESTMapping was not fully initialized
	// or if it was constructed manually. In this case, we assume the resource
	// is not namespaced.
	if r.Scope == nil {
		return false
	}

	return r.Scope.Name() == meta.RESTScopeNameNamespace
}
