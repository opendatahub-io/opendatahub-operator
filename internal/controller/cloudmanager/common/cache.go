package common

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
)

func CacheOptions(scheme *runtime.Scheme) cache.Options {
	cacheOptions := cache.Options{
		Scheme:            scheme,
		DefaultNamespaces: getManagedNamespaces(nil),
		DefaultTransform: func(in any) (any, error) {
			// Nilcheck managed fields to avoid hitting https://github.com/kubernetes/kubernetes/issues/124337
			if obj, err := meta.Accessor(in); err == nil && obj.GetManagedFields() != nil {
				obj.SetManagedFields(nil)
			}

			return in, nil
		},
	}

	return cacheOptions
}

func getManagedNamespaces(additionalConfig map[string]cache.Config) map[string]cache.Config {
	cacheConfig := map[string]cache.Config{
		NamespaceCertManagerOperator: {},
		NamespaceLWSOperator:         {},
		NamespaceSailOperator:        {},
	}

	for ns, cfg := range additionalConfig {
		cacheConfig[ns] = cfg
	}

	return cacheConfig
}
