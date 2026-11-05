package gateway_test

import (
	"bytes"
	"embed"
	"strings"
	"testing"
	"text/template"
)

//go:embed resources/kube-auth-proxy-oauth-deployment.tmpl.yaml resources/kube-auth-proxy-oidc-deployment.tmpl.yaml
var kubeAuthProxyTemplates embed.FS

// TestKubeAuthProxyTemplatesCipherSuiteGuard pins the empty-value guard on
// --tls-cipher-suite: cluster TLS profiles with no TLS 1.2 cipher list (for
// example TLS 1.3 profiles) must not render an empty flag value, which the
// proxy rejects at startup.
func TestKubeAuthProxyTemplatesCipherSuiteGuard(t *testing.T) {
	for _, name := range []string{
		"kube-auth-proxy-oauth-deployment.tmpl.yaml",
		"kube-auth-proxy-oidc-deployment.tmpl.yaml",
	} {
		tmpl := template.Must(template.ParseFS(kubeAuthProxyTemplates, "resources/"+name))
		for _, ciphers := range []string{"", "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384"} {
			var buf bytes.Buffer
			data := map[string]any{
				"TLSCertsMountPath":        "/etc/tls",
				"TLSMinVersion":            "VersionTLS13",
				"TLSCipherSuite":           ciphers,
				"EnableK8sTokenValidation": true,
				"RedirectURL":              "https://example.com",
				"UseSystemTrustStore":      true,
			}
			if err := tmpl.Execute(&buf, data); err != nil {
				t.Fatalf("%s ciphers=%q: %v", name, ciphers, err)
			}
			out := buf.String()
			if ciphers == "" && strings.Contains(out, "--tls-cipher-suite") {
				t.Errorf("%s: empty cipher list still rendered --tls-cipher-suite", name)
			}
			if ciphers != "" && !strings.Contains(out, "--tls-cipher-suite="+ciphers) {
				t.Errorf("%s: non-empty cipher list missing flag", name)
			}
		}
	}
}
