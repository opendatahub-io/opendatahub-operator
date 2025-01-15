package gc

import (
	"slices"
	"sync"

	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type Resource struct {
	meta.RESTMapping
}

func (r Resource) GroupVersionResource() schema.GroupVersionResource {
	return r.RESTMapping.Resource
}

func (r Resource) GroupVersionKind() schema.GroupVersionKind {
	return r.RESTMapping.GroupVersionKind
}

func (r Resource) String() string {
	return r.RESTMapping.Resource.String()
}

func (r Resource) IsNamespaced() bool {
	if r.Scope == nil {
		return false
	}

	return r.Scope.Name() == meta.RESTScopeNameNamespace
}

// We may want to introduce iterators (https://pkg.go.dev/iter) once moved to go 1.23

type Resources struct {
	lock  sync.RWMutex
	items []Resource
}

func (r *Resources) Set(resources []Resource) {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.items = resources
}
func (r *Resources) Get() []Resource {
	r.lock.RLock()
	defer r.lock.RUnlock()

	return slices.Clone(r.items)
}

func (r *Resources) Len() int {
	return len(r.items)
}

func MatchRule(resourceGroup string, apiRes metav1.APIResource, rule authorizationv1.ResourceRule) bool {
	for rgi := range rule.APIGroups {
		// Check if the resource group matches the rule group or is a wildcard, if not
		// discard it
		if resourceGroup != rule.APIGroups[rgi] && rule.APIGroups[rgi] != AnyResource {
			continue
		}

		for ri := range rule.Resources {
			// Check if the API resource name matches the rule resource or is a wildcard
			if apiRes.Name == rule.Resources[ri] || rule.Resources[ri] == AnyResource {
				return true
			}
		}
	}

	return false
}
