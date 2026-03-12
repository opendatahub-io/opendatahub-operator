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

	t.Run("label selector matches resources with infrastructure label", func(t *testing.T) {
		g := NewWithT(t)

		opts, err := app.DefaultCacheOptions(s)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(opts.ByObject).ToNot(BeEmpty(), "expected at least one selector for cluster-scoped resource types")

		for obj, byObj := range opts.ByObject {
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
}
