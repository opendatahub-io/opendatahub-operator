//nolint:testpackage
package gateway

import (
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

	. "github.com/onsi/gomega"
)

const (
	testProxyDomain    = "data-science-gateway.apps.test-cluster.com"
	testProxyIssuerURL = "https://keycloak.example.com/realms/test"
)

// getEnvoyFilterLuaCode is a helper function to extract Lua inline_code from EnvoyFilter for testing.
func getEnvoyFilterLuaCode(t *testing.T) string {
	t.Helper()
	g := NewWithT(t)
	ctx := t.Context()

	rr := &odhtypes.ReconciliationRequest{
		Client: setupTestClient(),
		Instance: &serviceApi.GatewayConfig{
			Spec: serviceApi.GatewayConfigSpec{},
		},
	}

	err := createEnvoyFilter(ctx, rr)
	g.Expect(err).NotTo(HaveOccurred(), "should create EnvoyFilter successfully")
	g.Expect(rr.Resources).To(HaveLen(1), "should create exactly one EnvoyFilter resource")

	// Get configPatches array
	configPatches, found, err := unstructured.NestedSlice(
		rr.Resources[0].Object,
		"spec", "configPatches",
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue(), "configPatches should exist")
	g.Expect(configPatches).To(HaveLen(3), "should have 3 config patches (ext_authz, lua, cluster)")

	// Get the Lua filter (second patch, index 1)
	luaPatch, ok := configPatches[1].(map[string]interface{})
	g.Expect(ok).To(BeTrue(), "Lua patch should be a map")

	// Extract inline_code
	inlineCode, found, err := unstructured.NestedString(
		luaPatch,
		"patch", "value", "typed_config", "inline_code",
	)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue(), "Lua filter inline_code should exist")

	return inlineCode
}

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
				Expire: metav1.Duration{Duration: 8 * time.Hour},
			},
			expectedExpire:  "8h0m0s",
			expectedRefresh: "1h",
		},
		{
			name: "custom refresh only",
			cookieConfig: &serviceApi.CookieConfig{
				Refresh: metav1.Duration{Duration: 30 * time.Minute},
			},
			expectedExpire:  "24h",
			expectedRefresh: "30m0s",
		},
		{
			name: "both custom values",
			cookieConfig: &serviceApi.CookieConfig{
				Expire:  metav1.Duration{Duration: 12 * time.Hour},
				Refresh: metav1.Duration{Duration: 45 * time.Minute},
			},
			expectedExpire:  "12h0m0s",
			expectedRefresh: "45m0s",
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
	g.Expect(args).To(ContainElement(fmt.Sprintf("--cookie-name=%s", OAuth2ProxyCookieName)))
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
		Expire:  metav1.Duration{Duration: 8 * time.Hour},
		Refresh: metav1.Duration{Duration: 30 * time.Minute},
	}

	args := buildBaseOAuth2ProxyArgs(cookieConfig, testProxyDomain)

	// Verify custom cookie settings are applied
	g.Expect(args).To(ContainElement("--cookie-expire=8h0m0s"), "custom cookie expiration")
	g.Expect(args).To(ContainElement("--cookie-refresh=30m0s"), "custom cookie refresh interval")

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

// TestCreateEnvoyFilter tests the EnvoyFilter creation with cookie stripping logic.
func TestCreateEnvoyFilter(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := t.Context()

	rr := &odhtypes.ReconciliationRequest{
		Client: setupTestClient(),
		Instance: &serviceApi.GatewayConfig{
			Spec: serviceApi.GatewayConfigSpec{},
		},
	}

	err := createEnvoyFilter(ctx, rr)
	g.Expect(err).NotTo(HaveOccurred(), "should create EnvoyFilter successfully")
	g.Expect(rr.Resources).To(HaveLen(1), "should create exactly one EnvoyFilter resource")

	envoyFilter := rr.Resources[0]
	g.Expect(envoyFilter.GetKind()).To(Equal("EnvoyFilter"))
	g.Expect(envoyFilter.GetName()).To(Equal("authn-filter"))
	g.Expect(envoyFilter.GetNamespace()).To(Equal("openshift-ingress"))
}

// TestCreateEnvoyFilterCookieNameReplacement tests that cookie name placeholder is correctly replaced.
func TestCreateEnvoyFilterCookieNameReplacement(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	inlineCode := getEnvoyFilterLuaCode(t)

	// Verify cookie name placeholder is replaced with actual cookie name
	g.Expect(inlineCode).NotTo(ContainSubstring("{{.CookieName}}"), "cookie name placeholder should be replaced")
	g.Expect(inlineCode).To(ContainSubstring(OAuth2ProxyCookieName), "should contain actual cookie name: %s", OAuth2ProxyCookieName)

	// Verify the Lua pattern uses the correct cookie name
	expectedPattern := "^" + OAuth2ProxyCookieName
	g.Expect(inlineCode).To(ContainSubstring(expectedPattern), "Lua pattern should match cookie name with ^ anchor")
}

// TestEnvoyFilterCookieStrippingLogic tests that the Lua code correctly strips OAuth2 proxy cookies.
func TestEnvoyFilterCookieStrippingLogic(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	inlineCode := getEnvoyFilterLuaCode(t)

	// Verify key cookie stripping logic components are present
	g.Expect(inlineCode).To(ContainSubstring("x-auth-request-access-token"),
		"should check for auth token before stripping cookies")
	g.Expect(inlineCode).To(ContainSubstring("cookie_header"),
		"should get cookie header")
	g.Expect(inlineCode).To(ContainSubstring("filtered_cookies"),
		"should filter cookies")
	g.Expect(inlineCode).To(ContainSubstring("cookie:match"),
		"should parse cookies using pattern matching")
	g.Expect(inlineCode).To(ContainSubstring("headers():replace(\"cookie\""),
		"should replace cookie header with filtered cookies")
	g.Expect(inlineCode).To(ContainSubstring("headers():remove(\"cookie\")"),
		"should remove cookie header if all cookies are filtered")

	// Verify the logic only strips cookies when auth token is present
	g.Expect(inlineCode).To(ContainSubstring("if access_token then"),
		"should only process cookies when auth token exists")
	g.Expect(inlineCode).To(ContainSubstring("If no auth token present, preserve cookies"),
		"should preserve cookies for ext_authz call")
}

// TestEnvoyFilterCookiePatternMatchesSplitCookies tests that the cookie pattern matches split cookies.
func TestEnvoyFilterCookiePatternMatchesSplitCookies(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	inlineCode := getEnvoyFilterLuaCode(t)

	// Verify the pattern uses ^ anchor to match cookies starting with the cookie name
	// This ensures split cookies like _oauth2_proxy_1, _oauth2_proxy_2 are matched
	expectedPattern := "^" + OAuth2ProxyCookieName
	g.Expect(inlineCode).To(ContainSubstring(expectedPattern),
		"pattern should use ^ anchor to match cookies starting with cookie name")

	// Verify comments mention split cookies
	g.Expect(inlineCode).To(ContainSubstring("_oauth2_proxy_1"),
		"should mention split cookies in comments")
	g.Expect(inlineCode).To(ContainSubstring("split cookies"),
		"should document split cookie handling")
}

// TestEnvoyFilterPreservesCookiesForAuthProxy tests that cookies are preserved for ext_authz calls.
func TestEnvoyFilterPreservesCookiesForAuthProxy(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	inlineCode := getEnvoyFilterLuaCode(t)

	// Verify cookies are preserved when no auth token is present (for ext_authz call)
	g.Expect(inlineCode).To(ContainSubstring("If no auth token present, preserve cookies"),
		"should preserve cookies for ext_authz authentication")
	g.Expect(inlineCode).To(ContainSubstring("if access_token then"),
		"should only strip cookies when auth token is present")
	g.Expect(inlineCode).To(ContainSubstring("ext_authz call to auth proxy"),
		"should document that cookies are needed for ext_authz")
}

// TestEnvoyFilterSetsAuthorizationHeader tests that the Lua filter sets Authorization header for upstream.
func TestEnvoyFilterSetsAuthorizationHeader(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	inlineCode := getEnvoyFilterLuaCode(t)

	// Verify Authorization header is set for upstream services
	g.Expect(inlineCode).To(ContainSubstring("authorization"),
		"should set Authorization header")
	g.Expect(inlineCode).To(ContainSubstring("Bearer"),
		"should use Bearer token format")
	g.Expect(inlineCode).To(ContainSubstring("x-forwarded-access-token"),
		"should also set x-forwarded-access-token header")
	g.Expect(inlineCode).To(ContainSubstring("Set headers for upstream services"),
		"should document header setting for upstream")
}

// TestEnvoyFilterRemovesCookieFromUpstreamRequests tests that _oauth2_proxy cookie is removed before forwarding to upstream.
func TestEnvoyFilterRemovesCookieFromUpstreamRequests(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	inlineCode := getEnvoyFilterLuaCode(t)

	// Verify the cookie stripping logic ensures upstream services don't receive _oauth2_proxy cookies
	// The logic should only strip cookies when forwarding to upstream (after auth succeeds)
	g.Expect(inlineCode).To(ContainSubstring("Strip OAuth2 proxy cookies only when forwarding to upstream services"),
		"should document that cookies are stripped only for upstream requests")
	g.Expect(inlineCode).To(ContainSubstring("Only keep cookies that don't match the OAuth2 proxy cookie pattern"),
		"should filter out OAuth2 proxy cookies")
	g.Expect(inlineCode).To(ContainSubstring("not cookie_name:match(cookie_pattern)"),
		"should use pattern matching to exclude OAuth2 proxy cookies")

	// Verify the pattern matches all variations (main cookie and split cookies)
	expectedPattern := "^" + OAuth2ProxyCookieName
	g.Expect(inlineCode).To(ContainSubstring(expectedPattern),
		"pattern should match cookies starting with %s (including split cookies)", OAuth2ProxyCookieName)

	// Verify cookies are only stripped after authentication (when access_token exists)
	g.Expect(inlineCode).To(ContainSubstring("if access_token then"),
		"should only strip cookies after authentication succeeds")
	g.Expect(inlineCode).To(ContainSubstring("we're now forwarding to upstream services"),
		"should confirm cookies are stripped when forwarding to upstream")
}

// TestGetGatewayAuthTimeout tests the timeout resolution priority and fallback logic.
func TestGetGatewayAuthTimeout(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	testCases := []struct {
		name            string
		gatewayConfig   *serviceApi.GatewayConfig
		expectedTimeout string
	}{
		{
			name:            "nil config returns default",
			gatewayConfig:   nil,
			expectedTimeout: "5s",
		},
		{
			name: "empty config returns default",
			gatewayConfig: &serviceApi.GatewayConfig{
				Spec: serviceApi.GatewayConfigSpec{},
			},
			expectedTimeout: "5s",
		},
		{
			name: "deprecated AuthTimeout field takes priority over AuthProxyTimeout",
			gatewayConfig: &serviceApi.GatewayConfig{
				Spec: serviceApi.GatewayConfigSpec{
					AuthTimeout:      "10s",
					AuthProxyTimeout: metav1.Duration{Duration: 15 * time.Second},
				},
			},
			expectedTimeout: "10s",
		},
		{
			name: "AuthProxyTimeout when deprecated field is empty",
			gatewayConfig: &serviceApi.GatewayConfig{
				Spec: serviceApi.GatewayConfigSpec{
					AuthProxyTimeout: metav1.Duration{Duration: 8 * time.Second},
				},
			},
			expectedTimeout: "8s",
		},
		{
			name: "deprecated AuthTimeout only",
			gatewayConfig: &serviceApi.GatewayConfig{
				Spec: serviceApi.GatewayConfigSpec{
					AuthTimeout: "20s",
				},
			},
			expectedTimeout: "20s",
		},
		{
			name: "AuthProxyTimeout only",
			gatewayConfig: &serviceApi.GatewayConfig{
				Spec: serviceApi.GatewayConfigSpec{
					AuthProxyTimeout: metav1.Duration{Duration: 25 * time.Second},
				},
			},
			expectedTimeout: "25s",
		},
		{
			name:            "default when all sources are empty",
			gatewayConfig:   &serviceApi.GatewayConfig{},
			expectedTimeout: "5s",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := getGatewayAuthTimeout(tc.gatewayConfig)
			g.Expect(result).To(Equal(tc.expectedTimeout))
		})
	}
}
