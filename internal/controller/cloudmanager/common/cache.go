package common

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
)

// DefaultCacheOptions builds cache.Options for the given scheme, watching the default
// managed namespaces shared by all cloud managers.
func DefaultCacheOptions(scheme *runtime.Scheme) cache.Options {
	nsConfig := map[string]cache.Config{
		NamespaceCertManagerOperator: {},
		NamespaceLWSOperator:         {},
		NamespaceSailOperator:        {},
	}

	defaultCacheOptions := cache.Options{
		Scheme:            scheme,
		DefaultNamespaces: nsConfig,
		DefaultTransform: func(in any) (any, error) {
			// Nilcheck managed fields to avoid hitting https://github.com/kubernetes/kubernetes/issues/124337
			if obj, err := meta.Accessor(in); err == nil && obj.GetManagedFields() != nil {
				obj.SetManagedFields(nil)
			}

			return in, nil
		},
	}

	return defaultCacheOptions
}
