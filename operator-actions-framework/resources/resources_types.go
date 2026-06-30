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
// for accessing common properties and relationships.
type Resource struct {
	meta.RESTMapping
}

func (r Resource) GroupVersionResource() schema.GroupVersionResource {
	return r.Resource
}

func (r Resource) GroupVersionKind() schema.GroupVersionKind {
	return r.RESTMapping.GroupVersionKind
}

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

func (r Resource) IsNamespaced() bool {
	if r.Scope == nil {
		return false
	}

	return r.Scope.Name() == meta.RESTScopeNameNamespace
}
