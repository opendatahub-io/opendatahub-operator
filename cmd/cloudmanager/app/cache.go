package app

import (
	"fmt"
	"maps"

	rbacv1 "k8s.io/api/rbac/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency/certmanager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// DefaultCacheOptions builds cache.Options for the given scheme, watching the default
// managed namespaces shared by all cloud managers and filtering cluster-scoped
// resources by the existence of the infrastructure part-of label.
func DefaultCacheOptions(scheme *runtime.Scheme) (cache.Options, error) {
	managedNamespaces := common.ManagedNamespaces()

	requirement, err := k8slabels.NewRequirement(labels.InfrastructurePartOf, selection.Exists, nil)
	if err != nil {
		return cache.Options{}, fmt.Errorf("failed to create label requirement for %s: %w", labels.InfrastructurePartOf, err)
	}

	labelSelector := k8slabels.NewSelector().Add(*requirement)

	clusterScopedConfig := cache.ByObject{
		Label: labelSelector,
	}

	defaultCacheConfig := cache.Config{LabelSelector: labelSelector}

	roleBindingCacheOption := cacheOptionsWithAdditionalNamespaces(managedNamespaces, map[string]cache.Config{
		common.NamespaceKubeSystem: defaultCacheConfig,
	})

	defaultNsConfig := make(map[string]cache.Config, len(managedNamespaces))
	for _, ns := range managedNamespaces {
		defaultNsConfig[ns] = defaultCacheConfig
	}
	bootstrapConfig := certmanager.DefaultBootstrapConfig()
	defaultNsConfig[bootstrapConfig.CertManagerNamespace] = defaultCacheConfig
	if operatorConfig := certmanager.BootstrapOperatorCertConfig(); operatorConfig.Namespace != "" {
		defaultNsConfig[operatorConfig.Namespace] = defaultCacheConfig
	}

	return cache.Options{
		Scheme:            scheme,
		DefaultNamespaces: defaultNsConfig,
		ByObject: map[client.Object]cache.ByObject{
			&rbacv1.ClusterRole{}:        clusterScopedConfig,
			&rbacv1.ClusterRoleBinding{}: clusterScopedConfig,
			// TODO: consider using a metadata-only cache for CRDs to reduce memory
			// usage now that all CRDs are cached (not just labeled ones).
			// See controller-runtime's support for metadata-only informers.
			&extv1.CustomResourceDefinition{}:            {},
			resources.GvkToUnstructured(gvk.RoleBinding): roleBindingCacheOption,
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

func cacheOptionsWithAdditionalNamespaces(managedNamespaces []string, extras map[string]cache.Config) cache.ByObject {
	nsConfig := make(map[string]cache.Config, len(managedNamespaces)+len(extras))
	for _, ns := range managedNamespaces {
		nsConfig[ns] = cache.Config{}
	}
	maps.Copy(nsConfig, extras)
	return cache.ByObject{
		Namespaces: nsConfig,
	}
}
