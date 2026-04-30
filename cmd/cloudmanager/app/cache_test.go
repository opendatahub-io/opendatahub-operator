//nolint:testpackage
package app

import (
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"

	. "github.com/onsi/gomega"
)

func Test_defaultCacheOptions(t *testing.T) {
	s := runtime.NewScheme()

	t.Run("uses label selector in DefaultNamespaces to filter namespaced resources", func(t *testing.T) {
		g := NewWithT(t)

		opts, err := defaultCacheOptions(s)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(opts.DefaultNamespaces).To(HaveLen(1),
			"expected DefaultNamespaces to have AllNamespaces config")

		allNsConfig, exists := opts.DefaultNamespaces[""]
		g.Expect(exists).To(BeTrue(), "expected AllNamespaces ('') key in DefaultNamespaces")
		g.Expect(allNsConfig.LabelSelector).ToNot(BeNil(),
			"expected LabelSelector to be set in DefaultNamespaces config")

		withLabel := k8slabels.Set{
			labels.InfrastructurePartOf: "azurekubernetesengine",
		}
		g.Expect(allNsConfig.LabelSelector.Matches(withLabel)).To(BeTrue(),
			"LabelSelector should match resources with %s label", labels.InfrastructurePartOf)

		withoutLabel := k8slabels.Set{
			"some-other-label": "value",
		}
		g.Expect(allNsConfig.LabelSelector.Matches(withoutLabel)).To(BeFalse(),
			"LabelSelector should not match resources without %s label", labels.InfrastructurePartOf)
	})

	t.Run("watches all namespaces with label filter", func(t *testing.T) {
		g := NewWithT(t)

		opts, err := defaultCacheOptions(s)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(opts.DefaultNamespaces).To(HaveLen(1),
			"DefaultNamespaces should have one entry for AllNamespaces")

		_, hasAllNamespaces := opts.DefaultNamespaces[""]
		g.Expect(hasAllNamespaces).To(BeTrue(),
			"DefaultNamespaces should use AllNamespaces ('') key to watch all namespaces")
	})

	t.Run("cluster-scoped resources have explicit label selectors", func(t *testing.T) {
		g := NewWithT(t)

		opts, err := defaultCacheOptions(s)
		g.Expect(err).ShouldNot(HaveOccurred())

		g.Expect(opts.ByObject).To(HaveLen(3),
			"expected ClusterRole, ClusterRoleBinding, and CRD in ByObject")

		// ClusterRole and ClusterRoleBinding should use the infrastructure label filter
		var clusterRoleConfig, clusterRoleBindingConfig, crdConfig *cache.ByObject
		for obj, config := range opts.ByObject {
			switch obj.(type) {
			case *rbacv1.ClusterRole:
				cfg := config
				clusterRoleConfig = &cfg
			case *rbacv1.ClusterRoleBinding:
				cfg := config
				clusterRoleBindingConfig = &cfg
			case *extv1.CustomResourceDefinition:
				cfg := config
				crdConfig = &cfg
			}
		}

		g.Expect(clusterRoleConfig).ToNot(BeNil(), "ClusterRole config not found in ByObject")
		g.Expect(clusterRoleConfig.Label).ToNot(BeNil(),
			"ClusterRole must have explicit label selector")
		g.Expect(clusterRoleConfig.Label.Matches(k8slabels.Set{
			labels.InfrastructurePartOf: "azurekubernetesengine",
		})).To(BeTrue(), "ClusterRole should match resources with %s label", labels.InfrastructurePartOf)
		g.Expect(clusterRoleConfig.Label.Matches(k8slabels.Set{})).To(BeFalse(),
			"ClusterRole should filter by %s label", labels.InfrastructurePartOf)

		g.Expect(clusterRoleBindingConfig).ToNot(BeNil(), "ClusterRoleBinding config not found in ByObject")
		g.Expect(clusterRoleBindingConfig.Label).ToNot(BeNil(),
			"ClusterRoleBinding must have explicit label selector")
		g.Expect(clusterRoleBindingConfig.Label.Matches(k8slabels.Set{
			labels.InfrastructurePartOf: "azurekubernetesengine",
		})).To(BeTrue(), "ClusterRoleBinding should match resources with %s label", labels.InfrastructurePartOf)
		g.Expect(clusterRoleBindingConfig.Label.Matches(k8slabels.Set{})).To(BeFalse(),
			"ClusterRoleBinding should filter by %s label", labels.InfrastructurePartOf)

		// CRDs should match everything (no filter)
		g.Expect(crdConfig).ToNot(BeNil(), "CRD config not found in ByObject")
		g.Expect(crdConfig.Label).ToNot(BeNil(),
			"CRD must have explicit label selector")
		g.Expect(crdConfig.Label.Matches(k8slabels.Set{})).To(BeTrue(),
			"CRD label selector should match all labels (no filter)")
		g.Expect(crdConfig.Label.Matches(k8slabels.Set{
			labels.InfrastructurePartOf: "azurekubernetesengine",
		})).To(BeTrue(), "CRD should match resources with any labels")
	})

	t.Run("uses provided scheme", func(t *testing.T) {
		g := NewWithT(t)

		opts, err := defaultCacheOptions(s)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(opts.Scheme).To(Equal(s))
	})

	t.Run("DefaultTransform clears ManagedFields", func(t *testing.T) {
		g := NewWithT(t)

		opts, err := defaultCacheOptions(s)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(opts.DefaultTransform).ShouldNot(BeNil())

		obj := &rbacv1.ClusterRole{}
		obj.SetManagedFields([]metav1.ManagedFieldsEntry{{Manager: "test"}})

		result, err := opts.DefaultTransform(obj)
		g.Expect(err).ShouldNot(HaveOccurred())

		transformed, ok := result.(*rbacv1.ClusterRole)
		g.Expect(ok).To(BeTrue())
		g.Expect(transformed.GetManagedFields()).To(BeNil())
	})
}
