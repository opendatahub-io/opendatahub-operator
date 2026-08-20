package v1alpha1

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

// TestGatewayIssuerURLValidationEnvtest verifies that the kubebuilder validation on
// OIDCConfig.IssuerURL (MinLength/MaxLength + Format=uri + Pattern `^https://[^?#\s]+$`
// + an XValidation CEL rule requiring a non-empty host) is
// enforced at admission time by a real API server. This is the single source of truth
// for the field's validation: it exercises the generated CRD schema end-to-end rather
// than a duplicated copy of the pattern.
//
// Scope note: the validation guarantees the https scheme, a non-empty host, no
// whitespace, and no query/fragment (an OIDC issuer identifier has none); Format=uri
// additionally rejects strings that do not parse as a URI (e.g. `{{`, backtick, pipe).
// It is NOT a comprehensive injection filter — values such as `https://host$(id).com`
// are valid URIs and are accepted. Downstream consumers must still treat the issuer URL
// as untrusted input.
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

	// MaxLength boundary strings: both are otherwise-valid HTTPS URLs padded in the
	// path with an allowed character, so length is the only thing under test.
	const maxLenBase = "https://example.com/"
	atMaxLength := maxLenBase + strings.Repeat("a", 2048-len(maxLenBase))
	overMaxLength := maxLenBase + strings.Repeat("a", 2049-len(maxLenBase))

	rejected := []struct {
		name string
		url  string
	}{
		{name: "non-HTTPS scheme", url: "http://insecure.example.com"},
		{name: "uppercase scheme", url: "HTTPS://example.com"},
		{name: "scheme only (no host)", url: "https://"},
		{name: "empty authority (leading slash)", url: "https:///realm"},
		{name: "empty host with port", url: "https://:443"},
		{name: "userinfo with empty host", url: "https://user@/realm"},
		{name: "missing double slash", url: "https:example.com"},
		{name: "whitespace in URL", url: "https://host name.com"},
		{name: "empty string", url: ""},
		{name: "not a valid URI (template braces)", url: "https://host{{.Value}}.com"},
		{name: "query string", url: "https://keycloak.example.com/realms/myorg?foo=bar"},
		{name: "fragment", url: "https://keycloak.example.com/realms/myorg#section"},
		{name: "over max length (2049)", url: overMaxLength},
	}

	for _, tc := range rejected {
		t.Run("rejected: "+tc.name, func(t *testing.T) {
			g := NewWithT(t)
			gw := validGatewayWithIssuerURL(tc.url)
			err := k8sClient.Create(ctx, gw)
			// GatewayConfig is a cluster-scoped singleton; if a case is wrongly
			// accepted, clean it up so it can't cascade AlreadyExists into later cases.
			if err == nil {
				_ = k8sClient.Delete(ctx, gw)
			}
			g.Expect(err).To(HaveOccurred())
			g.Expect(k8serrors.IsInvalid(err)).To(BeTrue())
			g.Expect(err.Error()).To(ContainSubstring("issuerURL"))
		})
	}

	accepted := []struct {
		name string
		url  string
	}{
		{name: "typical keycloak URL", url: "https://keycloak.example.com/realms/myorg"},
		{name: "with port", url: "https://auth.example.com:8443/realms/test"},
		{name: "userinfo with valid host", url: "https://user@auth.example.com/realm"},
		{name: "at max length (2048)", url: atMaxLength},
	}

	for _, tc := range accepted {
		t.Run("accepted: "+tc.name, func(t *testing.T) {
			g := NewWithT(t)
			gw := validGatewayWithIssuerURL(tc.url)
			g.Expect(k8sClient.Create(ctx, gw)).To(Succeed())
			g.Expect(k8sClient.Delete(ctx, gw)).To(Succeed())
		})
	}
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
