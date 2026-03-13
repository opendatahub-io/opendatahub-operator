package resources_test

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"

	. "github.com/onsi/gomega"
)

func newUnstructured(group, version, kind, namespace, name string) unstructured.Unstructured {
	u := unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: group, Version: version, Kind: kind})
	u.SetNamespace(namespace)
	u.SetName(name)
	return u
}

func TestSortByApplyOrder(t *testing.T) {
	t.Run("sorts CRDs before Deployments before unknown kinds", func(t *testing.T) {
		g := NewWithT(t)

		input := []unstructured.Unstructured{
			newUnstructured(gvk.CertManagerClusterIssuer.Group, gvk.CertManagerClusterIssuer.Version, gvk.CertManagerClusterIssuer.Kind, "", "my-issuer"),
			newUnstructured(gvk.Deployment.Group, gvk.Deployment.Version, gvk.Deployment.Kind, "ns", "my-deploy"),
			newUnstructured(gvk.CustomResourceDefinition.Group, gvk.CustomResourceDefinition.Version, gvk.CustomResourceDefinition.Kind, "", "my-crd"),
		}

		result, err := resources.SortByApplyOrder(context.Background(), input)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result).To(HaveLen(3))
		g.Expect(result[0].GetKind()).To(Equal("CustomResourceDefinition"))
		g.Expect(result[1].GetKind()).To(Equal("Deployment"))
		g.Expect(result[2].GetKind()).To(Equal("ClusterIssuer"))
	})

	t.Run("sorts webhooks last", func(t *testing.T) {
		g := NewWithT(t)

		input := []unstructured.Unstructured{
			newUnstructured("admissionregistration.k8s.io", "v1", "ValidatingWebhookConfiguration", "", "webhook"),
			newUnstructured(gvk.Namespace.Group, gvk.Namespace.Version, gvk.Namespace.Kind, "", "my-ns"),
			newUnstructured(gvk.Deployment.Group, gvk.Deployment.Version, gvk.Deployment.Kind, "ns", "my-deploy"),
		}

		result, err := resources.SortByApplyOrder(context.Background(), input)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result).To(HaveLen(3))
		g.Expect(result[0].GetKind()).To(Equal("Namespace"))
		g.Expect(result[1].GetKind()).To(Equal("Deployment"))
		g.Expect(result[2].GetKind()).To(Equal("ValidatingWebhookConfiguration"))
	})

	t.Run("unknown kinds placed in middle", func(t *testing.T) {
		g := NewWithT(t)

		input := []unstructured.Unstructured{
			newUnstructured(gvk.CertManagerCertificate.Group, gvk.CertManagerCertificate.Version, gvk.CertManagerCertificate.Kind, "cert-manager", "ca"),
			newUnstructured(gvk.Namespace.Group, gvk.Namespace.Version, gvk.Namespace.Kind, "", "my-ns"),
			newUnstructured("admissionregistration.k8s.io", "v1", "MutatingWebhookConfiguration", "", "webhook"),
		}

		result, err := resources.SortByApplyOrder(context.Background(), input)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result).To(HaveLen(3))
		g.Expect(result[0].GetKind()).To(Equal(gvk.Namespace.Kind))
		g.Expect(result[1].GetKind()).To(Equal(gvk.CertManagerCertificate.Kind))
		g.Expect(result[2].GetKind()).To(Equal("MutatingWebhookConfiguration"))
	})

	t.Run("empty input returns empty", func(t *testing.T) {
		g := NewWithT(t)

		result, err := resources.SortByApplyOrder(context.Background(), nil)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result).To(BeEmpty())
	})

	t.Run("stable sort preserves order for same kind", func(t *testing.T) {
		g := NewWithT(t)

		input := []unstructured.Unstructured{
			newUnstructured(gvk.Deployment.Group, gvk.Deployment.Version, gvk.Deployment.Kind, "ns", "deploy-b"),
			newUnstructured(gvk.Deployment.Group, gvk.Deployment.Version, gvk.Deployment.Kind, "ns", "deploy-a"),
		}

		result, err := resources.SortByApplyOrder(context.Background(), input)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result).To(HaveLen(2))
		// Same GVK + namespace → sorted by name
		g.Expect(result[0].GetName()).To(Equal("deploy-a"))
		g.Expect(result[1].GetName()).To(Equal("deploy-b"))
	})
}
