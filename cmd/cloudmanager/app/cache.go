package app

import (
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

// defaultCacheOptions builds cache.Options for the given scheme.
//
// Namespace Strategy:
// All namespaces are watched to support custom namespace configuration.
// Resources are filtered by the infrastructure.opendatahub.io/part-of label,
// so only labeled resources are cached.
//
// This approach is necessary because:
// - Dependency namespaces are configurable in the CR spec
// - Controller-runtime does not support dynamic namespace discovery
// - The cache must be initialized before any CRs exist
//
// CRDs bypass the label filter because they are cluster-scoped and do not
// carry the infrastructure part-of label.
func defaultCacheOptions(scheme *runtime.Scheme) (cache.Options, error) {
	requirement, err := k8slabels.NewRequirement(labels.InfrastructurePartOf, selection.Exists, nil)
	if err != nil {
		return cache.Options{}, fmt.Errorf("failed to create label requirement for %s: %w", labels.InfrastructurePartOf, err)
	}

	labelSelector := k8slabels.NewSelector().Add(*requirement)

	clusterScopedConfig := cache.ByObject{
		Label: labelSelector,
	}

	return cache.Options{
		Scheme: scheme,
		DefaultNamespaces: map[string]cache.Config{
			cache.AllNamespaces: {
				LabelSelector: labelSelector,
			},
		},
		ByObject: map[client.Object]cache.ByObject{
			&rbacv1.ClusterRole{}:        clusterScopedConfig,
			&rbacv1.ClusterRoleBinding{}: clusterScopedConfig,
			// TODO: consider using a metadata-only cache for CRDs to reduce memory
			// usage now that all CRDs are cached (not just labeled ones).
			// See controller-runtime's support for metadata-only informers.
			&extv1.CustomResourceDefinition{}: {
				Label: k8slabels.Everything(),
			},
		},
		DefaultTransform: cache.TransformStripManagedFields(),
	}, nil
}
