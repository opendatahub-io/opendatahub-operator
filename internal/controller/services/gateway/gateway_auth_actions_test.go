//nolint:testpackage
package gateway

import (
	"testing"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"

	. "github.com/onsi/gomega"
)

const (
	testProxyDomain    = "data-science-gateway.apps.test-cluster.com"
	testProxyIssuerURL = "https://keycloak.example.com/realms/test"
)

// TestGetCookieSettings tests the cookie settings helper function.
func TestGetCookieSettings(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	testCases := []struct {
		name            string
		cookieConfig    *serviceApi.CookieConfig
		expectedExpire  string
		expectedRefresh string
	}{
		{
			name:            "nil config returns defaults",
			cookieConfig:    nil,
			expectedExpire:  "24h",
			expectedRefresh: "1h",
		},
		{
			name:            "empty config returns defaults",
			cookieConfig:    &serviceApi.CookieConfig{},
			expectedExpire:  "24h",
			expectedRefresh: "1h",
		},
		{
			name: "custom expire only",
			cookieConfig: &serviceApi.CookieConfig{
				Expire: "8h",
			},
			expectedExpire:  "8h",
			expectedRefresh: "1h",
		},
		{
			name: "custom refresh only",
			cookieConfig: &serviceApi.CookieConfig{
				Refresh: "30m",
			},
			expectedExpire:  "24h",
			expectedRefresh: "30m",
		},
		{
			name: "both custom values",
			cookieConfig: &serviceApi.CookieConfig{
				Expire:  "12h",
				Refresh: "45m",
			},
			expectedExpire:  "12h",
			expectedRefresh: "45m",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			expire, refresh := getCookieSettings(tc.cookieConfig)
			g.Expect(expire).To(Equal(tc.expectedExpire))
			g.Expect(refresh).To(Equal(tc.expectedRefresh))
		})
	}
}

// TestBuildBaseOAuth2ProxyArgs tests common OAuth2 proxy arguments shared by both OIDC and OpenShift OAuth.
func TestBuildBaseOAuth2ProxyArgs(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	args := buildBaseOAuth2ProxyArgs(nil, testProxyDomain)

	// Verify argument count (20 base arguments)
	g.Expect(args).To(HaveLen(20), "should have 20 base arguments")

	// Core configuration
	g.Expect(args).To(ContainElement("--http-address=0.0.0.0:4180"))
	g.Expect(args).To(ContainElement("--https-address=0.0.0.0:8443"))
	g.Expect(args).To(ContainElement("--metrics-address=0.0.0.0:9000"))
	g.Expect(args).To(ContainElement("--email-domain=*"))
	g.Expect(args).To(ContainElement("--upstream=static://200"))
	g.Expect(args).To(ContainElement("--redirect-url=https://" + testProxyDomain + "/oauth2/callback"))

	// TLS configuration
	g.Expect(args).To(ContainElement("--tls-cert-file=/etc/tls/private/tls.crt"))
	g.Expect(args).To(ContainElement("--tls-key-file=/etc/tls/private/tls.key"))
	g.Expect(args).To(ContainElement("--use-system-trust-store=true"))

	// Cookie configuration - security and lifecycle (with new defaults)
	g.Expect(args).To(ContainElement("--cookie-expire=24h"), "cookie expires after 24 hours (default)")
	g.Expect(args).To(ContainElement("--cookie-refresh=1h"), "cookie refreshes every 1 hour (new default)")
	g.Expect(args).To(ContainElement("--cookie-secure=true"), "HTTPS only")
	g.Expect(args).To(ContainElement("--cookie-httponly=true"), "XSS protection")
	g.Expect(args).To(ContainElement("--cookie-samesite=lax"), "CSRF protection")
	g.Expect(args).To(ContainElement("--cookie-name=_oauth2_proxy"))
	g.Expect(args).To(ContainElement("--cookie-domain=" + testProxyDomain))

	// Security features
	g.Expect(args).To(ContainElement("--skip-jwt-bearer-tokens=true"), "allow bearer tokens to bypass OAuth login")
	g.Expect(args).To(ContainElement("--skip-provider-button"))
	g.Expect(args).To(ContainElement("--pass-access-token=true"))
	g.Expect(args).To(ContainElement("--set-xauthrequest=true"))
}

// TestBuildBaseOAuth2ProxyArgsWithCustomCookie tests custom cookie configuration.
func TestBuildBaseOAuth2ProxyArgsWithCustomCookie(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	cookieConfig := &serviceApi.CookieConfig{
		Expire:  "8h",
		Refresh: "30m",
	}

	args := buildBaseOAuth2ProxyArgs(cookieConfig, testProxyDomain)

	// Verify custom cookie settings are applied
	g.Expect(args).To(ContainElement("--cookie-expire=8h"), "custom cookie expiration")
	g.Expect(args).To(ContainElement("--cookie-refresh=30m"), "custom cookie refresh interval")

	// Verify other settings remain unchanged
	g.Expect(args).To(ContainElement("--cookie-secure=true"))
	g.Expect(args).To(ContainElement("--cookie-httponly=true"))
}

// TestBuildOIDCArgs tests OIDC-specific arguments.
func TestBuildOIDCArgs(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	oidcConfig := &serviceApi.OIDCConfig{
		IssuerURL: testProxyIssuerURL,
	}

	args := buildOIDCArgs(oidcConfig)

	g.Expect(args).To(HaveLen(3), "should have 3 OIDC-specific arguments")
	g.Expect(args).To(ContainElement("--provider=oidc"))
	g.Expect(args).To(ContainElement("--oidc-issuer-url=" + testProxyIssuerURL))
	g.Expect(args).To(ContainElement("--skip-oidc-discovery=false"), "enable OIDC discovery")
}

// TestBuildOpenShiftOAuthArgs tests OpenShift OAuth-specific arguments.
func TestBuildOpenShiftOAuthArgs(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	args := buildOpenShiftOAuthArgs()

	g.Expect(args).To(HaveLen(2), "should have 2 OpenShift OAuth arguments")
	g.Expect(args).To(ContainElement("--provider=openshift"))
	g.Expect(args).To(ContainElement("--scope=user:full"))
}
