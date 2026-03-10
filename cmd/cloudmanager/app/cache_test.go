package app_test

import (
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/cloudmanager/app"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"

	. "github.com/onsi/gomega"
)

func TestDefaultCacheOptions(t *testing.T) {
	g := NewWithT(t)
	s := runtime.NewScheme()

	t.Run("label selector matches resources with infrastructure label", func(t *testing.T) {
		g := NewWithT(t)

		opts, err := app.DefaultCacheOptions(s)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(opts.ByObject).ToNot(BeEmpty(), "expected at least one selector for cluster-scoped resource types")

		for obj, byObj := range opts.ByObject {
			if obj.GetObjectKind().GroupVersionKind() == gvk.RoleBinding {
				g.Expect(byObj.Label).To(BeNil(),
					"RoleBinding should use per-namespace label selectors, not a top-level label selector")
				continue
			}
			g.Expect(byObj.Label).ToNot(BeNil(),
				"label selector for %T should not be nil", obj)

			withLabel := k8slabels.Set{
				labels.InfrastructurePartOf: "azurekubernetesengine",
			}
			g.Expect(byObj.Label.Matches(withLabel)).To(BeTrue(),
				"label selector for %T should match resources with %s label", obj, labels.InfrastructurePartOf)

			withDifferentValue := k8slabels.Set{
				labels.InfrastructurePartOf: "coreweavekubernetesengine",
			}
			g.Expect(byObj.Label.Matches(withDifferentValue)).To(BeTrue(),
				"label selector for %T should match resources with any %s value", obj, labels.InfrastructurePartOf)

			withoutLabel := k8slabels.Set{
				"some-other-label": "value",
			}
			g.Expect(byObj.Label.Matches(withoutLabel)).To(BeFalse(),
				"label selector for %T should not match resources without %s label", obj, labels.InfrastructurePartOf)
		}
	})

	t.Run("uses provided scheme", func(t *testing.T) {
		opts, err := app.DefaultCacheOptions(s)
		g.Expect(err).ShouldNot(HaveOccurred())

		g.Expect(opts.Scheme).To(Equal(s))
	})

	t.Run("DefaultTransform clears ManagedFields", func(t *testing.T) {
		g := NewWithT(t)

		opts, err := app.DefaultCacheOptions(s)
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

	t.Run("RoleBinding cache covers managed namespaces and kube-system", func(t *testing.T) {
		g := NewWithT(t)

		opts, err := app.DefaultCacheOptions(s)
		g.Expect(err).ShouldNot(HaveOccurred())

		rbByObj := findByObjectGVK(g, opts.ByObject, gvk.RoleBinding)

		expectedNS := append(common.ManagedNamespaces(), common.NamespaceKubeSystem)
		g.Expect(rbByObj.Namespaces).To(HaveLen(len(expectedNS)))
		for _, ns := range expectedNS {
			g.Expect(rbByObj.Namespaces).To(HaveKey(ns),
				"RoleBinding namespaces should include %s", ns)
		}
	})

	t.Run("RoleBinding cache filters kube-system namespace by label selector", func(t *testing.T) {
		g := NewWithT(t)

		opts, err := app.DefaultCacheOptions(s)
		g.Expect(err).ShouldNot(HaveOccurred())

		rbByObj := findByObjectGVK(g, opts.ByObject, gvk.RoleBinding)
		ksConfig := rbByObj.Namespaces[common.NamespaceKubeSystem]

		g.Expect(ksConfig.LabelSelector).ShouldNot(BeNil(),
			"kube-system config should have a label selector")

		matching := k8slabels.Set{
			labels.InfrastructurePartOf: "azurekubernetesengine",
		}
		g.Expect(ksConfig.LabelSelector.Matches(matching)).To(BeTrue(),
			"kube-system label selector should match %v", matching)

		nonMatching := k8slabels.Set{
			"test-label": "test-value",
		}
		g.Expect(ksConfig.LabelSelector.Matches(nonMatching)).To(BeFalse(),
			"kube-system label selector should not match resources without %s label", labels.InfrastructurePartOf)
	})

	t.Run("RoleBinding cache does not filter managed namespaces by label selector", func(t *testing.T) {
		g := NewWithT(t)

		opts, err := app.DefaultCacheOptions(s)
		g.Expect(err).ShouldNot(HaveOccurred())

		rbByObj := findByObjectGVK(g, opts.ByObject, gvk.RoleBinding)

		for _, ns := range common.ManagedNamespaces() {
			nsConfig := rbByObj.Namespaces[ns]
			g.Expect(nsConfig.LabelSelector).To(BeNil(),
				"managed namespace %s should not have a label selector", ns)
		}
	})
}

func findByObjectGVK(g Gomega, byObject map[client.Object]cache.ByObject, target schema.GroupVersionKind) cache.ByObject {
	for obj, byObj := range byObject {
		if obj.GetObjectKind().GroupVersionKind() == target {
			return byObj
		}
	}

	g.Expect(true).To(BeFalse(), "expected GVK %s to be present in ByObject", target)

	return cache.ByObject{}
}
