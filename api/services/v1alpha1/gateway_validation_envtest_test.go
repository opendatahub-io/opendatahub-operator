package v1alpha1

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

	t.Run("certificate issuer kind is not defaulted by admission", func(t *testing.T) {
		g := NewWithT(t)
		gw := validGatewayWithIssuerURL("https://auth.example.com")
		gw.Spec.Certificate = &infrav1.CertificateSpec{
			Type:      infrav1.SelfSigned,
			IssuerRef: &infrav1.IssuerRef{Name: "tenant-issuer"},
		}
		g.Expect(k8sClient.Create(ctx, gw)).To(Succeed())
		t.Cleanup(func() { g.Expect(k8sClient.Delete(ctx, gw)).To(Succeed()) })
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(gw), gw)).To(Succeed())
		g.Expect(gw.Spec.Certificate.IssuerRef.Name).To(Equal("tenant-issuer"))
		g.Expect(gw.Spec.Certificate.IssuerRef.Kind).To(BeEmpty())
	})

	t.Run("certificate issuer name follows DNS subdomain naming", func(t *testing.T) {
		accepted := validGatewayWithIssuerURL("https://auth.example.com")
		accepted.Spec.Certificate = &infrav1.CertificateSpec{
			Type: infrav1.SelfSigned,
			IssuerRef: &infrav1.IssuerRef{
				Name: "tenant.issuer",
			},
		}
		g := NewWithT(t)
		g.Expect(k8sClient.Create(ctx, accepted)).To(Succeed())
		g.Expect(k8sClient.Delete(ctx, accepted)).To(Succeed())

		for _, issuerName := range []string{"bad/name", "Issuer Name"} {
			t.Run("rejected: "+issuerName, func(t *testing.T) {
				g := NewWithT(t)
				gw := validGatewayWithIssuerURL("https://auth.example.com")
				gw.Spec.Certificate = &infrav1.CertificateSpec{
					Type: infrav1.SelfSigned,
					IssuerRef: &infrav1.IssuerRef{
						Name: issuerName,
					},
				}

				err := k8sClient.Create(ctx, gw)
				if err == nil {
					g.Expect(k8sClient.Delete(ctx, gw)).To(Succeed())
				}
				g.Expect(err).To(HaveOccurred())
				g.Expect(k8serrors.IsInvalid(err)).To(BeTrue())
				g.Expect(err.Error()).To(ContainSubstring("issuerRef.name"))
			})
		}
	})

	for _, test := range []struct {
		name      string
		invalid   bool
		duplicate bool
	}{
		{name: "alpha"},
		{name: strings.Repeat("a", MaxAdditionalGatewayNameLength)},
		{name: strings.Repeat("a", MaxAdditionalGatewayNameLength+1), invalid: true},
		{name: "1alpha", invalid: true},
		{name: "bad_name", invalid: true},
		{name: "alpha-", invalid: true},
		{name: DefaultGatewayName, invalid: true},
		{name: XKSDefaultGatewayName, invalid: true},
		{name: "duplicate", invalid: true, duplicate: true},
	} {
		t.Run("additional Gateway name: "+test.name, func(t *testing.T) {
			g := NewWithT(t)
			gw := validGatewayWithIssuerURL("https://auth.example.com")
			gw.Spec.IngressMode = IngressModeOcpRoute
			gw.Spec.AdditionalIngresses = AdditionalIngresses{{
				Name: test.name, Hostname: "alpha.example.com", IngressControllerName: "shard-alpha",
				RouteLabels: map[string]string{"example.com/ingress": "alpha"},
			}}
			if test.duplicate {
				gw.Spec.AdditionalIngresses = append(gw.Spec.AdditionalIngresses, AdditionalIngress{
					Name: test.name, Hostname: "beta.example.com", IngressControllerName: "shard-beta",
					RouteLabels: map[string]string{"example.com/ingress": "beta"},
				})
			}
			err := k8sClient.Create(ctx, gw)
			if err == nil {
				g.Expect(k8sClient.Delete(ctx, gw)).To(Succeed())
			}
			if test.invalid {
				g.Expect(k8serrors.IsInvalid(err)).To(BeTrue())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}

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
				g.Expect(k8sClient.Delete(ctx, gw)).To(Succeed())
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

func TestGatewayAdditionalIngressValidationEnvtest(t *testing.T) {
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

	authCases := []struct {
		name    string
		auth    string
		maximum int32
		wantErr string
		wantKey string
	}{
		{name: "omitted auth", maximum: 10},
		{name: "empty auth", auth: `{}`, maximum: 10},
		{name: "maximum equals fixed minimum", auth: `{"maxReplicas":2}`, maximum: 2},
		{name: "configured maximum", auth: `{"maxReplicas":4}`, maximum: 4},
		{name: "largest maximum", auth: `{"maxReplicas":10}`, maximum: 10},
		{name: "zero maximum", auth: `{"maxReplicas":0}`, wantErr: "maxReplicas"},
		{name: "maximum below minimum", auth: `{"maxReplicas":1}`, wantErr: "maxReplicas"},
		{name: "maximum above limit", auth: `{"maxReplicas":11}`, wantErr: "maxReplicas"},
		{name: "missing Secret key", auth: `{"oidc":{"clientID":"alpha-client","clientSecretRef":{"name":"alpha-oidc"}}}`, wantErr: "key"},
		{name: "OIDC explicit key", auth: `{"oidc":{"clientID":"alpha-client","clientSecretRef":{"name":"alpha-oidc","key":"clientSecret"}}}`, maximum: 10, wantKey: "clientSecret"},
		{name: "OIDC custom key", auth: `{"oidc":{"clientID":"alpha-client","clientSecretRef":{"name":"alpha-oidc","key":"credentials"}}}`, maximum: 10, wantKey: "credentials"},
		{name: "required Secret", auth: `{"oidc":{"clientID":"alpha-client","clientSecretRef":{"name":"alpha-oidc","key":"clientSecret","optional":false}}}`, maximum: 10, wantKey: "clientSecret"},
		{name: "optional Secret", auth: `{"oidc":{"clientID":"alpha-client","clientSecretRef":{"name":"alpha-oidc","key":"clientSecret","optional":true}}}`, wantErr: "clientSecretRef.optional"},
		{name: "missing client ID", auth: `{"oidc":{"clientSecretRef":{"name":"alpha-oidc","key":"clientSecret"}}}`, wantErr: "clientID"},
		{name: "empty client ID", auth: `{"oidc":{"clientID":"","clientSecretRef":{"name":"alpha-oidc","key":"clientSecret"}}}`, wantErr: "clientID"},
		{name: "missing Secret reference", auth: `{"oidc":{"clientID":"alpha-client"}}`, wantErr: "clientSecretRef"},
		{name: "missing Secret name", auth: `{"oidc":{"clientID":"alpha-client","clientSecretRef":{"key":"clientSecret"}}}`, wantErr: "clientSecretRef.name"},
		{name: "empty Secret name", auth: `{"oidc":{"clientID":"alpha-client","clientSecretRef":{"name":"","key":"clientSecret"}}}`, wantErr: "clientSecretRef.name"},
		{name: "empty Secret key", auth: `{"oidc":{"clientID":"alpha-client","clientSecretRef":{"name":"alpha-oidc","key":""}}}`, wantErr: "key"},
	}
	for _, target := range []string{"additional ingress", "default gateway"} {
		for _, tc := range authCases {
			if target == "default gateway" && strings.Contains(tc.auth, "oidc") {
				continue
			}
			t.Run(target+" auth: "+tc.name, func(t *testing.T) {
				g := NewWithT(t)
				ingress := map[string]interface{}{
					"name": "alpha", "hostname": "alpha.example.com",
					"ingressControllerName": "shard-a", "routeLabels": map[string]interface{}{"example.com/ingress": "alpha"},
				}
				var auth map[string]interface{}
				if tc.auth != "" {
					g.Expect(json.Unmarshal([]byte(tc.auth), &auth)).To(Succeed())
					ingress["auth"] = auth
				}
				config := &unstructured.Unstructured{Object: map[string]interface{}{
					"apiVersion": GroupVersion.String(), "kind": GatewayConfigKind,
					"metadata": map[string]interface{}{"name": GatewayConfigName},
					"spec": map[string]interface{}{
						"ingressMode": string(IngressModeOcpRoute), "additionalIngresses": []interface{}{ingress},
					},
				}}
				if target == "default gateway" {
					delete(ingress, "auth")
					if maximum, ok := auth["maxReplicas"]; ok {
						config.Object["spec"].(map[string]interface{})["authProxyMaxReplicas"] = maximum
					}
				}
				err := k8sClient.Create(ctx, config)
				if err == nil {
					t.Cleanup(func() { g.Expect(k8sClient.Delete(ctx, config)).To(Succeed()) })
				}
				if tc.wantErr != "" {
					g.Expect(k8serrors.IsInvalid(err)).To(BeTrue())
					wantErr := tc.wantErr
					if target == "default gateway" {
						wantErr = "authProxyMaxReplicas"
					}
					g.Expect(err.Error()).To(ContainSubstring(wantErr))
					return
				}
				g.Expect(err).NotTo(HaveOccurred())
				var observed GatewayConfig
				g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: GatewayConfigName}, &observed)).To(Succeed())
				ingressAuth := observed.Spec.AdditionalIngresses[0].Auth
				if target == "default gateway" {
					ingressAuth = AdditionalIngressAuth{MaxReplicas: observed.Spec.AuthProxyMaxReplicas}
				}
				g.Expect(ingressAuth.MaxReplicas).NotTo(BeNil())
				g.Expect(*ingressAuth.MaxReplicas).To(Equal(tc.maximum))
				if tc.wantKey != "" {
					g.Expect(ingressAuth.OIDC).NotTo(BeNil())
					g.Expect(ingressAuth.OIDC.ClientSecretRef.Key).To(Equal(tc.wantKey))
					g.Expect(ingressAuth.OIDC.SecretNamespace).To(BeEmpty())
				} else {
					g.Expect(ingressAuth.OIDC).To(BeNil())
				}
			})
		}

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
