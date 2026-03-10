package app_test

import (
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8slabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/cloudmanager/app"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"

	. "github.com/onsi/gomega"
)

func TestDefaultCacheOptions(t *testing.T) {
	g := NewWithT(t)
	s := runtime.NewScheme()

	t.Run("label selector matches correct value", func(t *testing.T) {
		g := NewWithT(t)

		opts, err := app.DefaultCacheOptions(s, "azurekubernetesengine")
		g.Expect(err).ShouldNot(HaveOccurred())

		for obj, byObj := range opts.ByObject {
			matching := k8slabels.Set{
				labels.InfrastructurePartOf: "azurekubernetesengine",
			}
			g.Expect(byObj.Label.Matches(matching)).To(BeTrue(),
				"label selector for %T should match %v", obj, matching)

			nonMatching := k8slabels.Set{
				labels.InfrastructurePartOf: "coreweavekubernetesengine",
			}
			g.Expect(byObj.Label.Matches(nonMatching)).To(BeFalse(),
				"label selector for %T should not match %v", obj, nonMatching)
		}
	})

	t.Run("normalizes infraPartOfValue to lowercase and trimmed", func(t *testing.T) {
		g := NewWithT(t)

		opts, err := app.DefaultCacheOptions(s, "  AzureKubernetesEngine  ")
		g.Expect(err).ShouldNot(HaveOccurred())

		for obj, byObj := range opts.ByObject {
			matching := k8slabels.Set{
				labels.InfrastructurePartOf: "azurekubernetesengine",
			}
			g.Expect(byObj.Label.Matches(matching)).To(BeTrue(),
				"label selector for %T should match normalized value %v", obj, matching)
		}
	})

	t.Run("returns error on empty infraPartOfValue", func(t *testing.T) {
		g := NewWithT(t)

		_, err := app.DefaultCacheOptions(s, "")
		g.Expect(err).Should(HaveOccurred())
	})

	t.Run("returns error on whitespace-only infraPartOfValue", func(t *testing.T) {
		g := NewWithT(t)

		_, err := app.DefaultCacheOptions(s, "   ")
		g.Expect(err).Should(HaveOccurred())
	})

	t.Run("uses provided scheme", func(t *testing.T) {
		opts, err := app.DefaultCacheOptions(s, "azurekubernetesengine")
		g.Expect(err).ShouldNot(HaveOccurred())

		g.Expect(opts.Scheme).To(Equal(s))
	})

	t.Run("DefaultTransform clears ManagedFields", func(t *testing.T) {
		g := NewWithT(t)

		opts, err := app.DefaultCacheOptions(s, "azurekubernetesengine")
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
