package app

import (
	"fmt"

	rbacv1 "k8s.io/api/rbac/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

// DefaultCacheOptions builds cache.Options for the given scheme, watching the default
// managed namespaces shared by all cloud managers and filtering cluster-scoped
// resources by the existence of the infrastructure part-of label.
func DefaultCacheOptions(scheme *runtime.Scheme) (cache.Options, error) {
	nsConfig := make(map[string]cache.Config, len(common.ManagedNamespaces()))
	for _, ns := range common.ManagedNamespaces() {
		nsConfig[ns] = cache.Config{}
	}

	requirement, err := k8slabels.NewRequirement(labels.InfrastructurePartOf, selection.Exists, nil)
	if err != nil {
		return cache.Options{}, fmt.Errorf("failed to create label requirement for %s: %w", labels.InfrastructurePartOf, err)
	}

	labelSelector := k8slabels.NewSelector().Add(*requirement)

	clusterScopedConfig := cache.ByObject{
		Label: labelSelector,
	}

	return cache.Options{
		Scheme:            scheme,
		DefaultNamespaces: nsConfig,
		ByObject: map[client.Object]cache.ByObject{
			&rbacv1.ClusterRole{}:             clusterScopedConfig,
			&rbacv1.ClusterRoleBinding{}:      clusterScopedConfig,
			&extv1.CustomResourceDefinition{}: clusterScopedConfig,
		},
		DefaultTransform: func(in any) (any, error) {
			// Nilcheck managed fields to avoid hitting https://github.com/kubernetes/kubernetes/issues/124337
			if obj, err := meta.Accessor(in); err == nil && obj.GetManagedFields() != nil {
				obj.SetManagedFields(nil)
			}

			return in, nil
		},
	}, nil
}
