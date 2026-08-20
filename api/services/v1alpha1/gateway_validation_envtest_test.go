package v1alpha1

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// TestGatewayIssuerURLValidationEnvtest verifies that the kubebuilder Pattern validation on
// OIDCConfig.IssuerURL is enforced at admission time by a real API server.
// This complements the pure-regex unit tests in gateway_types_test.go by proving
// the CRD schema actually wires the pattern into OpenAPI validation.
func TestGatewayIssuerURLValidationEnvtest(t *testing.T) {
	logf.SetLogger(zap.New(zap.WriteTo(os.Stdout), zap.UseDevMode(true)))

	g := NewWithT(t)
	ctx := context.Background()

	projectDir, err := envtestutil.FindProjectRoot()
	g.Expect(err).NotTo(HaveOccurred())

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join(projectDir, "config", "crd", "bases"),
		},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(cfg).ToNot(BeNil())
	defer func() {
		g.Expect(testEnv.Stop()).To(Succeed())
	}()

	k8sClient, err := client.New(cfg, client.Options{Scheme: gatewayTestScheme()})
	g.Expect(err).ToNot(HaveOccurred())

	t.Run("non-HTTPS issuer URL is rejected", func(t *testing.T) {
		g := NewWithT(t)
		gw := validGatewayWithIssuerURL("http://insecure.example.com")
		err := k8sClient.Create(ctx, gw)
		g.Expect(err).To(HaveOccurred())
		g.Expect(k8serrors.IsInvalid(err)).To(BeTrue())
		g.Expect(err.Error()).To(ContainSubstring("issuerURL"))
	})

	t.Run("shell injection in issuer hostname is rejected", func(t *testing.T) {
		g := NewWithT(t)
		gw := validGatewayWithIssuerURL("https://host;echo pwned.com")
		err := k8sClient.Create(ctx, gw)
		g.Expect(err).To(HaveOccurred())
		g.Expect(k8serrors.IsInvalid(err)).To(BeTrue())
	})

	t.Run("Go template injection in issuer hostname is rejected", func(t *testing.T) {
		g := NewWithT(t)
		gw := validGatewayWithIssuerURL("https://host{{.Value}}.com")
		err := k8sClient.Create(ctx, gw)
		g.Expect(err).To(HaveOccurred())
		g.Expect(k8serrors.IsInvalid(err)).To(BeTrue())
	})

	t.Run("command substitution in issuer hostname is rejected", func(t *testing.T) {
		g := NewWithT(t)
		gw := validGatewayWithIssuerURL("https://host$(id).com")
		err := k8sClient.Create(ctx, gw)
		g.Expect(err).To(HaveOccurred())
		g.Expect(k8serrors.IsInvalid(err)).To(BeTrue())
	})

	t.Run("empty issuer URL is rejected", func(t *testing.T) {
		g := NewWithT(t)
		gw := validGatewayWithIssuerURL("")
		err := k8sClient.Create(ctx, gw)
		g.Expect(err).To(HaveOccurred())
		g.Expect(k8serrors.IsInvalid(err)).To(BeTrue())
	})

	t.Run("valid HTTPS issuer URL is accepted", func(t *testing.T) {
		g := NewWithT(t)
		gw := validGatewayWithIssuerURL("https://keycloak.example.com/realms/myorg")
		g.Expect(k8sClient.Create(ctx, gw)).To(Succeed())
		g.Expect(k8sClient.Delete(ctx, gw)).To(Succeed())
	})

	t.Run("valid HTTPS issuer URL with port is accepted", func(t *testing.T) {
		g := NewWithT(t)
		gw := validGatewayWithIssuerURL("https://auth.example.com:8443/realms/test")
		g.Expect(k8sClient.Create(ctx, gw)).To(Succeed())
		g.Expect(k8sClient.Delete(ctx, gw)).To(Succeed())
	})
}

func gatewayTestScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = scheme.AddToScheme(s)
	_ = SchemeBuilder.AddToScheme(s)
	return s
}

func validGatewayWithIssuerURL(issuerURL string) *GatewayConfig {
	return &GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: GatewayConfigName,
		},
		Spec: GatewayConfigSpec{
			OIDC: &OIDCConfig{
				IssuerURL: issuerURL,
				ClientID:  "test-client",
				ClientSecretRef: corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "test-secret",
					},
					Key: "client-secret",
				},
			},
		},
	}
}
