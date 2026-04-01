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

	t.Run("cert-manager dependency ordering with SortByApplyOrderWithCertificates", func(t *testing.T) {
		g := NewWithT(t)

		input := []unstructured.Unstructured{
			// Mixed order input to test comprehensive ordering
			newUnstructured(gvk.Deployment.Group, gvk.Deployment.Version, gvk.Deployment.Kind, "app-ns", "consuming-app"),
			newUnstructured("admissionregistration.k8s.io", "v1", "ValidatingWebhookConfiguration", "", "webhook"),
			newUnstructured(gvk.CertManagerCertificate.Group, gvk.CertManagerCertificate.Version, gvk.CertManagerCertificate.Kind, "cert-manager", "ca-cert"),
			newUnstructured(gvk.Service.Group, gvk.Service.Version, gvk.Service.Kind, "app-ns", "app-service"),
			newUnstructured(gvk.CertManagerClusterIssuer.Group, gvk.CertManagerClusterIssuer.Version, gvk.CertManagerClusterIssuer.Kind, "", "ca-issuer"),
			newUnstructured(gvk.Namespace.Group, gvk.Namespace.Version, gvk.Namespace.Kind, "", "app-ns"),
			newUnstructured(gvk.CertManagerIssuer.Group, gvk.CertManagerIssuer.Version, gvk.CertManagerIssuer.Kind, "cert-manager", "self-signed-issuer"),
			newUnstructured(gvk.CustomResourceDefinition.Group, gvk.CustomResourceDefinition.Version, gvk.CustomResourceDefinition.Kind, "", "certificates.cert-manager.io"),
		}

		result, err := resources.SortByApplyOrderWithCertificates(context.Background(), input)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result).To(HaveLen(8))

		// Expected ordering (RHOAIENG-53513 requirement):
		// Certificate BEFORE Deployment to reduce "transient errors"
		// 1. Foundation resources: Namespace, CustomResourceDefinition (upstream decides)
		// 2. Standard early resources: Service (upstream decides)
		// 3. cert-manager resources: ClusterIssuer, Issuer, Certificate (inserted before workloads)
		// 4. Workload resources: Deployment (comes after cert-manager to reduce race conditions)
		// 5. Webhook resources: ValidatingWebhookConfiguration (upstream puts these last)

		g.Expect(result[0].GetKind()).To(Equal("Namespace"))
		g.Expect(result[1].GetKind()).To(Equal("CustomResourceDefinition"))
		g.Expect(result[2].GetKind()).To(Equal("Service"))
		g.Expect(result[3].GetKind()).To(Equal("ClusterIssuer"))
		g.Expect(result[4].GetKind()).To(Equal("Issuer"))
		g.Expect(result[5].GetKind()).To(Equal("Certificate"))
		g.Expect(result[6].GetKind()).To(Equal("Deployment"))
		g.Expect(result[7].GetKind()).To(Equal("ValidatingWebhookConfiguration"))
	})

	t.Run("cert-manager ordering works without Services", func(t *testing.T) {
		g := NewWithT(t)

		// Test scenario with NO Services - cert-manager should still work
		input := []unstructured.Unstructured{
			newUnstructured(gvk.Deployment.Group, gvk.Deployment.Version, gvk.Deployment.Kind, "app-ns", "consuming-app"),
			newUnstructured("admissionregistration.k8s.io", "v1", "ValidatingWebhookConfiguration", "", "webhook"),
			newUnstructured(gvk.CertManagerCertificate.Group, gvk.CertManagerCertificate.Version, gvk.CertManagerCertificate.Kind, "cert-manager", "ca-cert"),
			newUnstructured(gvk.CertManagerClusterIssuer.Group, gvk.CertManagerClusterIssuer.Version, gvk.CertManagerClusterIssuer.Kind, "", "ca-issuer"),
			newUnstructured(gvk.Namespace.Group, gvk.Namespace.Version, gvk.Namespace.Kind, "", "app-ns"),
			newUnstructured(gvk.CustomResourceDefinition.Group, gvk.CustomResourceDefinition.Version, gvk.CustomResourceDefinition.Kind, "", "certificates.cert-manager.io"),
		}

		result, err := resources.SortByApplyOrderWithCertificates(context.Background(), input)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(result).To(HaveLen(6))

		// Expected ordering without Services:
		// 1. Foundation: Namespace, CRD
		// 2. cert-manager: ClusterIssuer, Certificate (inserted after foundation)
		// 3. Workloads: Deployment
		// 4. Webhooks: ValidatingWebhookConfiguration

		g.Expect(result[0].GetKind()).To(Equal("Namespace"))
		g.Expect(result[1].GetKind()).To(Equal("CustomResourceDefinition"))
		g.Expect(result[2].GetKind()).To(Equal("ClusterIssuer"))
		g.Expect(result[3].GetKind()).To(Equal("Certificate"))
		g.Expect(result[4].GetKind()).To(Equal("Deployment"))
		g.Expect(result[5].GetKind()).To(Equal("ValidatingWebhookConfiguration"))
	})
}
