//nolint:testpackage
package gateway

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency/certmanager"

	. "github.com/onsi/gomega"
)

func TestResolveIssuerRef(t *testing.T) {
	tests := []struct {
		name         string
		envName      string
		envKind      string
		cert         *infrav1.CertificateSpec
		expectedName string
		expectedKind string
	}{
		{
			name:         "defaults when nothing set",
			expectedName: "opendatahub-ca-issuer",
			expectedKind: certmanager.DefaultIssuerRefKind,
		},
		{
			name:         "env overrides defaults",
			envName:      "rhai-ca-issuer",
			envKind:      "Issuer",
			expectedName: "rhai-ca-issuer",
			expectedKind: "Issuer",
		},
		{
			name:         "spec issuerRef overrides env",
			envName:      "rhai-ca-issuer",
			cert:         &infrav1.CertificateSpec{IssuerRef: &infrav1.IssuerRef{Name: "tenant-issuer", Kind: "Issuer"}},
			expectedName: "tenant-issuer",
			expectedKind: "Issuer",
		},
		{
			name:         "empty spec fields fall through to env/default",
			envName:      "rhai-ca-issuer",
			cert:         &infrav1.CertificateSpec{IssuerRef: &infrav1.IssuerRef{}},
			expectedName: "rhai-ca-issuer",
			expectedKind: certmanager.DefaultIssuerRefKind,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			if tt.envName != "" {
				t.Setenv(certmanager.EnvCAIssuerName, tt.envName)
			}
			if tt.envKind != "" {
				t.Setenv(certmanager.EnvIssuerRefKind, tt.envKind)
			}

			name, kind := resolveIssuerRef(tt.cert)
			g.Expect(name).To(Equal(tt.expectedName))
			g.Expect(kind).To(Equal(tt.expectedKind))
		})
	}
}

func TestBuildCertManagerCertificate(t *testing.T) {
	g := NewWithT(t)

	cert, err := buildCertManagerCertificate("my-cert", "rh-ai-gateway", "my-tls", []string{"rh-ai.example.com"}, "rhai-ca-issuer", "ClusterIssuer")
	g.Expect(err).ToNot(HaveOccurred())

	g.Expect(cert.GroupVersionKind()).To(Equal(gvk.CertManagerCertificate))
	g.Expect(cert.GetName()).To(Equal("my-cert"))
	g.Expect(cert.GetNamespace()).To(Equal("rh-ai-gateway"))

	secretName, _, err := unstructured.NestedString(cert.Object, "spec", "secretName")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(secretName).To(Equal("my-tls"))

	dnsNames, _, err := unstructured.NestedStringSlice(cert.Object, "spec", "dnsNames")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(dnsNames).To(ConsistOf("rh-ai.example.com"))

	issuerName, _, err := unstructured.NestedString(cert.Object, "spec", "issuerRef", "name")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(issuerName).To(Equal("rhai-ca-issuer"))

	issuerKind, _, err := unstructured.NestedString(cert.Object, "spec", "issuerRef", "kind")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(issuerKind).To(Equal("ClusterIssuer"))

	issuerGroup, _, err := unstructured.NestedString(cert.Object, "spec", "issuerRef", "group")
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(issuerGroup).To(Equal(gvk.CertManagerCertificate.Group))
}
