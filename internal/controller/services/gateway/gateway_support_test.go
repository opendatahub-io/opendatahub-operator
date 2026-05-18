//go:build !integration

//nolint:testpackage
package gateway

import (
	"context"
	"fmt"
	"testing"

	oauthv1 "github.com/openshift/api/oauth/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

	. "github.com/onsi/gomega"
)

// Test constants for domain, hostname, and auth values used across multiple tests.
const (
	testDomain                    = "apps.example.com"
	testHostnameDefault           = "rh-ai.apps.example.com"
	testHostnameLegacy            = "data-science-gateway.apps.example.com"
	testHostnameCustom            = "abc.apps.example.com"
	testHostnameCustomSubdomain   = "custom.apps.example.com"
	testSubdomainCustom           = "custom"
	testSubdomainMyGateway        = "my-gateway"
	testGatewayName               = "test-gateway"
	testAuthClientID              = "client-id"
	testAuthClientSecret          = "client-secret"
	testAuthCookieSecret          = "cookie-secret"
	testAuthClientIDDifferent     = "different-client-id"
	testAuthClientSecretDifferent = "different-client-secret"
	testAuthCookieSecretDifferent = "different-cookie-secret"
	testCookieExpireDefault       = "24h0m0s"
	testCookieRefreshDefault      = "1h0m0s"
	testAuthTimeoutDefault        = "5s"
	testOIDCSecretName            = "oidc-secret"
)

// TestGetCertificateType tests the getCertificateType helper function.
func TestGetCertificateType(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	testCases := []struct {
		name          string
		gatewayConfig *serviceApi.GatewayConfig
		expectedType  string
		description   string
	}{
		{
			name:          "returns default when gatewayConfig is nil",
			gatewayConfig: nil,
			expectedType:  string(infrav1.OpenshiftDefaultIngress),
			description:   "should return OpenShift default when gatewayConfig is nil",
		},
		{
			name: "returns default when certificate is nil",
			gatewayConfig: &serviceApi.GatewayConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: testGatewayName,
				},
				Spec: serviceApi.GatewayConfigSpec{},
			},
			expectedType: string(infrav1.OpenshiftDefaultIngress),
			description:  "should return OpenShift default when no certificate specified",
		},
		{
			name: "returns certificate type when specified",
			gatewayConfig: &serviceApi.GatewayConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: testGatewayName,
				},
				Spec: serviceApi.GatewayConfigSpec{
					Certificate: &infrav1.CertificateSpec{
						Type: infrav1.SelfSigned,
					},
				},
			},
			expectedType: string(infrav1.SelfSigned),
			description:  "should return the specified certificate type",
		},
		{
			name: "returns provided certificate type",
			gatewayConfig: &serviceApi.GatewayConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: testGatewayName,
				},
				Spec: serviceApi.GatewayConfigSpec{
					Certificate: &infrav1.CertificateSpec{
						Type:       infrav1.Provided,
						SecretName: "my-cert",
					},
				},
			},
			expectedType: string(infrav1.Provided),
			description:  "should return provided certificate type",
		},
		{
			name: "empty certificate type defaults to OpenshiftDefaultIngress",
			gatewayConfig: &serviceApi.GatewayConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: testGatewayName,
				},
				Spec: serviceApi.GatewayConfigSpec{
					Certificate: &infrav1.CertificateSpec{
						Type: "",
					},
				},
			},
			expectedType: string(infrav1.OpenshiftDefaultIngress),
			description:  "should return OpenShift default when certificate type is empty string",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := getCertificateType(tc.gatewayConfig)
			g.Expect(result).To(Equal(tc.expectedType), tc.description)
		})
	}
}

// TestGetGatewayAuthProxyTimeout tests the getGatewayAuthProxyTimeout function.
func TestGetGatewayAuthProxyTimeout(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	testCases := []struct {
		name            string
		gatewayConfig   *serviceApi.GatewayConfig
		expectedTimeout string
		description     string
	}{
		{
			name:            "returns default when gatewayConfig is nil",
			gatewayConfig:   nil,
			expectedTimeout: testAuthTimeoutDefault,
			description:     "should return default 5s when gatewayConfig is nil",
		},
		{
			name: "returns default when no timeout specified",
			gatewayConfig: &serviceApi.GatewayConfig{
				Spec: serviceApi.GatewayConfigSpec{},
			},
			expectedTimeout: testAuthTimeoutDefault,
			description:     "should return default 5s when no timeout specified",
		},
		{
			name: "returns deprecated AuthTimeout when specified",
			gatewayConfig: &serviceApi.GatewayConfig{
				Spec: serviceApi.GatewayConfigSpec{
					AuthTimeout: "10s",
				},
			},
			expectedTimeout: "10s",
			description:     "should return deprecated AuthTimeout value for backward compatibility",
		},
		{
			name: "returns AuthProxyTimeout when specified",
			gatewayConfig: &serviceApi.GatewayConfig{
				Spec: serviceApi.GatewayConfigSpec{
					AuthProxyTimeout: metav1.Duration{Duration: 15000000000}, // 15s in nanoseconds
				},
			},
			expectedTimeout: "15s",
			description:     "should return AuthProxyTimeout value",
		},
		{
			name: "prefers deprecated AuthTimeout over AuthProxyTimeout",
			gatewayConfig: &serviceApi.GatewayConfig{
				Spec: serviceApi.GatewayConfigSpec{
					AuthTimeout:      "20s",
					AuthProxyTimeout: metav1.Duration{Duration: 15000000000}, // 15s
				},
			},
			expectedTimeout: "20s",
			description:     "should prefer deprecated AuthTimeout for backward compatibility",
		},
		{
			name: "zero duration for AuthProxyTimeout falls back to default",
			gatewayConfig: &serviceApi.GatewayConfig{
				Spec: serviceApi.GatewayConfigSpec{
					AuthProxyTimeout: metav1.Duration{Duration: 0},
				},
			},
			expectedTimeout: testAuthTimeoutDefault,
			description:     "should return default 5s when AuthProxyTimeout is zero duration",
		},
		{
			name: "empty AuthTimeout string with zero AuthProxyTimeout",
			gatewayConfig: &serviceApi.GatewayConfig{
				Spec: serviceApi.GatewayConfigSpec{
					AuthTimeout:      "",
					AuthProxyTimeout: metav1.Duration{Duration: 0},
				},
			},
			expectedTimeout: testAuthTimeoutDefault,
			description:     "should return default 5s when both fields are empty/zero",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := getGatewayAuthProxyTimeout(tc.gatewayConfig)
			g.Expect(result).To(Equal(tc.expectedTimeout), tc.description)
		})
	}
}

// TestCalculateAuthConfigHash tests the calculateAuthConfigHash function.
func TestCalculateAuthConfigHash(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	// Create initial secret
	secret1 := &corev1.Secret{
		Data: map[string][]byte{
			"OAUTH2_PROXY_CLIENT_ID":     []byte(testAuthClientID),
			"OAUTH2_PROXY_CLIENT_SECRET": []byte(testAuthClientSecret),
			"OAUTH2_PROXY_COOKIE_SECRET": []byte(testAuthCookieSecret),
		},
	}
	hash1 := calculateAuthConfigHash(secret1)

	// Change client ID
	secret2 := &corev1.Secret{
		Data: map[string][]byte{
			"OAUTH2_PROXY_CLIENT_ID":     []byte(testAuthClientIDDifferent),
			"OAUTH2_PROXY_CLIENT_SECRET": []byte(testAuthClientSecret),
			"OAUTH2_PROXY_COOKIE_SECRET": []byte(testAuthCookieSecret),
		},
	}
	hash2 := calculateAuthConfigHash(secret2)
	g.Expect(hash2).NotTo(Equal(hash1), "hash should change when client ID changes")

	// Change client secret
	secret3 := &corev1.Secret{
		Data: map[string][]byte{
			"OAUTH2_PROXY_CLIENT_ID":     []byte(testAuthClientID),
			"OAUTH2_PROXY_CLIENT_SECRET": []byte(testAuthClientSecretDifferent),
			"OAUTH2_PROXY_COOKIE_SECRET": []byte(testAuthCookieSecret),
		},
	}
	hash3 := calculateAuthConfigHash(secret3)
	g.Expect(hash3).NotTo(Equal(hash1), "hash should change when client secret changes")

	// Change cookie secret
	secret4 := &corev1.Secret{
		Data: map[string][]byte{
			"OAUTH2_PROXY_CLIENT_ID":     []byte(testAuthClientID),
			"OAUTH2_PROXY_CLIENT_SECRET": []byte(testAuthClientSecret),
			"OAUTH2_PROXY_COOKIE_SECRET": []byte(testAuthCookieSecretDifferent),
		},
	}
	hash4 := calculateAuthConfigHash(secret4)
	g.Expect(hash4).NotTo(Equal(hash1), "hash should change when cookie secret changes")

	// All hashes should be unique
	g.Expect(hash2).NotTo(Equal(hash3), "different changes should produce different hashes")
	g.Expect(hash2).NotTo(Equal(hash4), "different changes should produce different hashes")
	g.Expect(hash3).NotTo(Equal(hash4), "different changes should produce different hashes")

	// Verify same values produce same hash (deterministic)
	secret5 := &corev1.Secret{
		Data: map[string][]byte{
			"OAUTH2_PROXY_CLIENT_ID":     []byte(testAuthClientID),
			"OAUTH2_PROXY_CLIENT_SECRET": []byte(testAuthClientSecret),
			"OAUTH2_PROXY_COOKIE_SECRET": []byte(testAuthCookieSecret),
		},
	}
	hash5 := calculateAuthConfigHash(secret5)
	g.Expect(hash5).To(Equal(hash1), "same secret values should produce same hash")
}

// TestCalculateRedirectConfigHash tests the CalculateRedirectConfigHash function.
func TestCalculateRedirectConfigHash(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	hash1 := CalculateRedirectConfigHash(testHostnameDefault)
	g.Expect(hash1).To(MatchRegexp("^[0-9a-f]{64}$"), "hash should be 64 hex chars")
	g.Expect(hash1).To(HaveLen(64))

	hash2 := CalculateRedirectConfigHash(testHostnameCustom)
	g.Expect(hash2).NotTo(Equal(hash1), "different hostnames should produce different hashes")

	hash3 := CalculateRedirectConfigHash(testHostnameDefault)
	g.Expect(hash3).To(Equal(hash1), "same hostname should produce same hash")
}

// TestIsGatewayReady tests the isGatewayReady helper function.
func TestIsGatewayReady(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	testCases := []struct {
		name        string
		gateway     *gwapiv1.Gateway
		expectReady bool
		description string
	}{
		{
			name:        "nil gateway is not ready",
			gateway:     nil,
			expectReady: false,
			description: "should return false for nil gateway",
		},
		{
			name: "gateway with Accepted condition true is ready",
			gateway: &gwapiv1.Gateway{
				Status: gwapiv1.GatewayStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(gwapiv1.GatewayConditionAccepted),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			expectReady: true,
			description: "should return true when Accepted condition is true",
		},
		{
			name: "gateway with Accepted condition false is not ready",
			gateway: &gwapiv1.Gateway{
				Status: gwapiv1.GatewayStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(gwapiv1.GatewayConditionAccepted),
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
			expectReady: false,
			description: "should return false when Accepted condition is false",
		},
		{
			name: "gateway with no conditions is not ready",
			gateway: &gwapiv1.Gateway{
				Status: gwapiv1.GatewayStatus{
					Conditions: []metav1.Condition{},
				},
			},
			expectReady: false,
			description: "should return false when no conditions present",
		},
		{
			name: "gateway with different condition is not ready",
			gateway: &gwapiv1.Gateway{
				Status: gwapiv1.GatewayStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(gwapiv1.GatewayConditionProgrammed),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			expectReady: false,
			description: "should return false when only non-Accepted conditions present",
		},
		{
			name: "gateway with multiple conditions including Accepted true",
			gateway: &gwapiv1.Gateway{
				Status: gwapiv1.GatewayStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(gwapiv1.GatewayConditionProgrammed),
							Status: metav1.ConditionFalse,
						},
						{
							Type:   string(gwapiv1.GatewayConditionAccepted),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			expectReady: true,
			description: "should return true when Accepted condition is true regardless of other conditions",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := isGatewayReady(tc.gateway)
			g.Expect(result).To(Equal(tc.expectReady), tc.description)
		})
	}
}

// TestGetFQDN tests the GetFQDN function with user-provided domain.
func TestGetFQDN(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	testCases := []struct {
		name           string
		gatewayConfig  *serviceApi.GatewayConfig
		expectedDomain string
		expectError    bool
		description    string
	}{
		{
			name: "user-provided domain is used",
			gatewayConfig: &serviceApi.GatewayConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: serviceApi.GatewayConfigName,
				},
				Spec: serviceApi.GatewayConfigSpec{
					Domain: testDomain,
				},
			},
			expectedDomain: DefaultGatewaySubdomain + "." + testDomain,
			expectError:    false,
			description:    "should use user-provided domain and prepend default gateway name",
		},
		{
			name: "custom subdomain with user-provided domain",
			gatewayConfig: &serviceApi.GatewayConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: serviceApi.GatewayConfigName,
				},
				Spec: serviceApi.GatewayConfigSpec{
					Domain:    testDomain,
					Subdomain: testSubdomainMyGateway,
				},
			},
			expectedDomain: testSubdomainMyGateway + "." + testDomain,
			expectError:    false,
			description:    "should use custom subdomain with user-provided domain",
		},
		{
			name: "whitespace subdomain falls back to default",
			gatewayConfig: &serviceApi.GatewayConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name: serviceApi.GatewayConfigName,
				},
				Spec: serviceApi.GatewayConfigSpec{
					Domain:    testDomain,
					Subdomain: "   ",
				},
			},
			expectedDomain: DefaultGatewaySubdomain + "." + testDomain,
			expectError:    false,
			description:    "should fall back to default when subdomain is whitespace",
		},
		{
			name:           "nil gatewayConfig falls back to cluster domain",
			gatewayConfig:  nil,
			expectedDomain: "",
			expectError:    true,
			description:    "should return error when gatewayConfig is nil and no cluster domain available",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			client := setupTestClient().Build()

			domain, err := GetFQDN(ctx, client, tc.gatewayConfig)

			if tc.expectError {
				g.Expect(err).To(HaveOccurred(), tc.description)
			} else {
				g.Expect(err).NotTo(HaveOccurred(), tc.description)
				g.Expect(domain).To(Equal(tc.expectedDomain), tc.description)
			}
		})
	}
}

// TestGetCookieSettings tests the getCookieSettings helper function.
func TestGetCookieSettings(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	testCases := []struct {
		name            string
		cookieConfig    *serviceApi.CookieConfig
		expectedExpire  string
		expectedRefresh string
		description     string
	}{
		{
			name:            "returns defaults when cookieConfig is nil",
			cookieConfig:    nil,
			expectedExpire:  testCookieExpireDefault,
			expectedRefresh: testCookieRefreshDefault,
			description:     "should return 24h and 1h when cookieConfig is nil",
		},
		{
			name: "returns defaults when both durations are zero",
			cookieConfig: &serviceApi.CookieConfig{
				Expire:  metav1.Duration{Duration: 0},
				Refresh: metav1.Duration{Duration: 0},
			},
			expectedExpire:  testCookieExpireDefault,
			expectedRefresh: testCookieRefreshDefault,
			description:     "should return defaults when durations are zero (handles cookie: {} case)",
		},
		{
			name: "returns custom values when specified",
			cookieConfig: &serviceApi.CookieConfig{
				Expire:  metav1.Duration{Duration: 48 * 3600 * 1000000000}, // 48h in nanoseconds
				Refresh: metav1.Duration{Duration: 30 * 60 * 1000000000},   // 30m in nanoseconds
			},
			expectedExpire:  "48h0m0s",
			expectedRefresh: "30m0s",
			description:     "should return custom values when specified",
		},
		{
			name: "returns mixed default and custom values",
			cookieConfig: &serviceApi.CookieConfig{
				Expire:  metav1.Duration{Duration: 72 * 3600 * 1000000000}, // 72h custom
				Refresh: metav1.Duration{Duration: 0},                      // default 1h
			},
			expectedExpire:  "72h0m0s",
			expectedRefresh: testCookieRefreshDefault,
			description:     "should return custom expire and default refresh",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			expire, refresh := getCookieSettings(tc.cookieConfig)
			g.Expect(expire).To(Equal(tc.expectedExpire), tc.description)
			g.Expect(refresh).To(Equal(tc.expectedRefresh), tc.description)
		})
	}
}

// TestComputeLegacyRedirectInfo tests the computeLegacyRedirectInfo helper function.
func TestComputeLegacyRedirectInfo(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	// Default subdomain enables legacy redirect
	info := computeLegacyRedirectInfo(nil, testHostnameDefault)
	g.Expect(info.LegacySubdomain).To(Equal(LegacyGatewaySubdomain))
	g.Expect(info.LegacyHostname).To(Equal(testHostnameLegacy))

	// Legacy subdomain disables redirect (empty legacy fields)
	legacyConfig := &serviceApi.GatewayConfig{
		Spec: serviceApi.GatewayConfigSpec{Subdomain: LegacyGatewaySubdomain},
	}
	info = computeLegacyRedirectInfo(legacyConfig, testHostnameLegacy)
	g.Expect(info.LegacySubdomain).To(BeEmpty())
	g.Expect(info.LegacyHostname).To(BeEmpty())

	// Custom subdomain still enables redirect
	customConfig := &serviceApi.GatewayConfig{
		Spec: serviceApi.GatewayConfigSpec{Subdomain: testSubdomainCustom},
	}
	info = computeLegacyRedirectInfo(customConfig, testHostnameCustomSubdomain)
	g.Expect(info.LegacyHostname).To(Equal(testHostnameLegacy))
}

// TestHPATemplateConstant tests that the HPA template constant is correctly defined.
func TestHPATemplateConstant(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	g.Expect(kubeAuthProxyHPATemplate).To(Equal("resources/kube-auth-proxy-hpa.tmpl.yaml"), "HPA template path should be correct")
}

// TestGetAuthProxySecretValuesOverridesStaleClientID verifies that when a
// kube-auth-proxy-creds secret already exists with a stale client ID (e.g.
// "odh" from a prior version), getAuthProxySecretValues returns the desired
// client ID for the current auth mode instead of the stale value.
func TestGetAuthProxySecretValuesOverridesStaleClientID(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	staleClientID := "odh"
	existingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      KubeAuthProxySecretsName,
			Namespace: GatewayNamespace,
		},
		Data: map[string][]byte{
			EnvClientID:     []byte(staleClientID),
			EnvClientSecret: []byte(testAuthClientSecret),
			EnvCookieSecret: []byte(testAuthCookieSecret),
		},
	}

	cli := setupTestClient().WithObjects(existingSecret).Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	clientID, clientSecret, cookieSecret, err := getAuthProxySecretValues(ctx, rr, cluster.AuthModeIntegratedOAuth, nil)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(clientID).To(Equal(AuthClientID), "must return the current AuthClientID, not the stale value from the secret")
	g.Expect(clientSecret).To(Equal(testAuthClientSecret), "must preserve existing client secret")
	g.Expect(cookieSecret).To(Equal(testAuthCookieSecret), "must preserve existing cookie secret")
}

// TestGetAuthProxySecretValuesRejectsNilOIDCConfig verifies that calling
// getAuthProxySecretValues with OIDC auth mode but nil oidcConfig returns
// an error instead of panicking with a nil pointer dereference.
func TestGetAuthProxySecretValuesRejectsNilOIDCConfig(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	cli := setupTestClient().Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	//nolint:dogsled // only the error matters in this test
	_, _, _, err := getAuthProxySecretValues(ctx, rr, cluster.AuthModeOIDC, nil)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("OIDC auth mode requires oidcConfig with non-empty ClientID and ClientSecretRef"))
}

// TestGetAuthProxySecretValuesRejectsEmptyOIDCClientID verifies that calling
// getAuthProxySecretValues with OIDC auth mode but an empty ClientID returns
// an error instead of writing an empty OAUTH2_PROXY_CLIENT_ID to the secret.
func TestGetAuthProxySecretValuesRejectsEmptyOIDCClientID(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	cli := setupTestClient().Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	oidcConfig := &serviceApi.OIDCConfig{
		ClientID: "",
		ClientSecretRef: corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "my-secret"},
			Key:                  "clientSecret",
		},
	}

	//nolint:dogsled // only the error matters in this test
	_, _, _, err := getAuthProxySecretValues(ctx, rr, cluster.AuthModeOIDC, oidcConfig)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("OIDC auth mode requires oidcConfig with non-empty ClientID and ClientSecretRef"))
}

// TestGetAuthProxySecretValuesRejectsEmptyOIDCClientSecretRef verifies that calling
// getAuthProxySecretValues with OIDC auth mode but an empty ClientSecretRef.Name
// returns an error.
func TestGetAuthProxySecretValuesRejectsEmptyOIDCClientSecretRef(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	cli := setupTestClient().Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	oidcConfig := &serviceApi.OIDCConfig{
		ClientID: "my-oidc-client",
	}

	//nolint:dogsled // only the error matters in this test
	_, _, _, err := getAuthProxySecretValues(ctx, rr, cluster.AuthModeOIDC, oidcConfig)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("OIDC auth mode requires oidcConfig with non-empty ClientID and ClientSecretRef"))
}

// TestGetAuthProxySecretValuesOIDCOverridesStaleClientID verifies that when a
// kube-auth-proxy-creds secret exists with a stale client ID, OIDC mode returns
// the desired client ID from oidcConfig instead of the stale value.
func TestGetAuthProxySecretValuesOIDCOverridesStaleClientID(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	oidcClientID := "my-oidc-client"
	oidcSecretName := "oidc-client-secret" //nolint:gosec // test fixture, not a real credential
	oidcSecretValue := "oidc-secret-value" //nolint:gosec // test fixture, not a real credential

	existingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      KubeAuthProxySecretsName,
			Namespace: GatewayNamespace,
		},
		Data: map[string][]byte{
			EnvClientID:     []byte("stale-oidc-client"),
			EnvClientSecret: []byte(testAuthClientSecret),
			EnvCookieSecret: []byte(testAuthCookieSecret),
		},
	}

	externalSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      oidcSecretName,
			Namespace: GatewayNamespace,
		},
		Data: map[string][]byte{
			"clientSecret": []byte(oidcSecretValue),
		},
	}

	cli := setupTestClient().WithObjects(existingSecret, externalSecret).Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	oidcConfig := &serviceApi.OIDCConfig{
		ClientID: oidcClientID,
		ClientSecretRef: corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: oidcSecretName},
			Key:                  "clientSecret",
		},
	}

	clientID, clientSecret, cookieSecret, err := getAuthProxySecretValues(ctx, rr, cluster.AuthModeOIDC, oidcConfig)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(clientID).To(Equal(oidcClientID), "must return OIDC client ID, not the stale value from the secret")
	g.Expect(clientSecret).To(Equal(oidcSecretValue), "must reload client secret from ClientSecretRef")
	g.Expect(cookieSecret).To(Equal(testAuthCookieSecret), "must preserve existing cookie secret")
}

// TestGetAuthProxySecretValuesPreservesCurrentClientID verifies that when the
// secret already contains the correct client ID, it is returned unchanged.
func TestGetAuthProxySecretValuesPreservesCurrentClientID(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	existingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      KubeAuthProxySecretsName,
			Namespace: GatewayNamespace,
		},
		Data: map[string][]byte{
			EnvClientID:     []byte(AuthClientID),
			EnvClientSecret: []byte(testAuthClientSecret),
			EnvCookieSecret: []byte(testAuthCookieSecret),
		},
	}

	cli := setupTestClient().WithObjects(existingSecret).Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	clientID, clientSecret, cookieSecret, err := getAuthProxySecretValues(ctx, rr, cluster.AuthModeIntegratedOAuth, nil)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(clientID).To(Equal(AuthClientID))
	g.Expect(clientSecret).To(Equal(testAuthClientSecret))
	g.Expect(cookieSecret).To(Equal(testAuthCookieSecret))
}

// TestDeleteLegacyOAuthClientNotFound verifies that deleteLegacyOAuthClient
// returns nil when no legacy OAuthClient exists (the common steady-state case).
func TestDeleteLegacyOAuthClientNotFound(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	gatewayConfig := &serviceApi.GatewayConfig{
		Spec: serviceApi.GatewayConfigSpec{
			Domain: testDomain,
		},
	}

	cli := setupTestClient().Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	err := deleteLegacyOAuthClient(ctx, rr, gatewayConfig)
	g.Expect(err).NotTo(HaveOccurred())
}

// TestDeleteLegacyOAuthClientMatchingRedirectURI verifies that deleteLegacyOAuthClient
// deletes the legacy "odh" OAuthClient when its redirect URI matches the expected
// gateway hostname, confirming it was created by the gateway controller.
func TestDeleteLegacyOAuthClientMatchingRedirectURI(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	gatewayConfig := &serviceApi.GatewayConfig{
		Spec: serviceApi.GatewayConfigSpec{
			Domain: testDomain,
		},
	}

	legacyClient := &oauthv1.OAuthClient{
		ObjectMeta: metav1.ObjectMeta{
			Name: LegacyAuthClientID,
		},
		RedirectURIs: []string{
			"https://" + testHostnameDefault + "/oauth2/callback",
		},
	}

	cli := setupTestClient().WithObjects(legacyClient).Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	err := deleteLegacyOAuthClient(ctx, rr, gatewayConfig)
	g.Expect(err).NotTo(HaveOccurred())

	// Verify the legacy client was deleted
	deleted := &oauthv1.OAuthClient{}
	err = cli.Get(ctx, types.NamespacedName{Name: LegacyAuthClientID}, deleted)
	g.Expect(k8serr.IsNotFound(err)).To(BeTrue(), "legacy OAuthClient should be deleted")
}

// TestDeleteLegacyOAuthClientLegacyHostname verifies that deleteLegacyOAuthClient
// also matches against the legacy hostname (data-science-gateway.*) redirect URI.
func TestDeleteLegacyOAuthClientLegacyHostname(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	gatewayConfig := &serviceApi.GatewayConfig{
		Spec: serviceApi.GatewayConfigSpec{
			Domain: testDomain,
		},
	}

	legacyClient := &oauthv1.OAuthClient{
		ObjectMeta: metav1.ObjectMeta{
			Name: LegacyAuthClientID,
		},
		RedirectURIs: []string{
			"https://" + testHostnameLegacy + "/oauth2/callback",
		},
	}

	cli := setupTestClient().WithObjects(legacyClient).Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	err := deleteLegacyOAuthClient(ctx, rr, gatewayConfig)
	g.Expect(err).NotTo(HaveOccurred())

	deleted := &oauthv1.OAuthClient{}
	err = cli.Get(ctx, types.NamespacedName{Name: LegacyAuthClientID}, deleted)
	g.Expect(k8serr.IsNotFound(err)).To(BeTrue(), "legacy OAuthClient with legacy hostname redirect should be deleted")
}

// TestDeleteLegacyOAuthClientNonGatewaySkipped verifies that deleteLegacyOAuthClient
// does NOT delete an OAuthClient named "odh" when its redirect URI does not match
// any expected gateway hostname. This protects user-created OAuthClients.
func TestDeleteLegacyOAuthClientNonGatewaySkipped(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	gatewayConfig := &serviceApi.GatewayConfig{
		Spec: serviceApi.GatewayConfigSpec{
			Domain: testDomain,
		},
	}

	unrelatedClient := &oauthv1.OAuthClient{
		ObjectMeta: metav1.ObjectMeta{
			Name: LegacyAuthClientID,
		},
		RedirectURIs: []string{
			"https://unrelated-app.example.com/callback",
		},
	}

	cli := setupTestClient().WithObjects(unrelatedClient).Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	err := deleteLegacyOAuthClient(ctx, rr, gatewayConfig)
	g.Expect(err).NotTo(HaveOccurred())

	// Verify the client was NOT deleted
	preserved := &oauthv1.OAuthClient{}
	err = cli.Get(ctx, types.NamespacedName{Name: LegacyAuthClientID}, preserved)
	g.Expect(err).NotTo(HaveOccurred(), "unrelated OAuthClient named %q must not be deleted", LegacyAuthClientID)
}

// TestDeleteLegacyOAuthClientNoRedirectURIs verifies that deleteLegacyOAuthClient
// does not delete a legacy OAuthClient with no redirect URIs.
func TestDeleteLegacyOAuthClientNoRedirectURIs(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	gatewayConfig := &serviceApi.GatewayConfig{
		Spec: serviceApi.GatewayConfigSpec{
			Domain: testDomain,
		},
	}

	legacyClient := &oauthv1.OAuthClient{
		ObjectMeta: metav1.ObjectMeta{
			Name: LegacyAuthClientID,
		},
		RedirectURIs: []string{},
	}

	cli := setupTestClient().WithObjects(legacyClient).Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	err := deleteLegacyOAuthClient(ctx, rr, gatewayConfig)
	g.Expect(err).NotTo(HaveOccurred())

	preserved := &oauthv1.OAuthClient{}
	err = cli.Get(ctx, types.NamespacedName{Name: LegacyAuthClientID}, preserved)
	g.Expect(err).NotTo(HaveOccurred(), "OAuthClient with no redirect URIs must not be deleted")
}

// TestGetAuthProxySecretValuesGeneratesNewSecrets verifies that when no
// existing kube-auth-proxy-creds secret exists, getAuthProxySecretValues
// generates new client secret and cookie secret values.
func TestGetAuthProxySecretValuesGeneratesNewSecrets(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	cli := setupTestClient().Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	clientID, clientSecret, cookieSecret, err := getAuthProxySecretValues(ctx, rr, cluster.AuthModeIntegratedOAuth, nil)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(clientID).To(Equal(AuthClientID))
	g.Expect(clientSecret).NotTo(BeEmpty(), "should generate a client secret")
	g.Expect(cookieSecret).NotTo(BeEmpty(), "should generate a cookie secret")
}

// TestGetAuthProxySecretValuesOIDCPreservesCookieSecret verifies that in OIDC
// mode, when an existing secret has a cookie secret, that cookie secret is
// preserved (to avoid invalidating user sessions) while the client secret is
// reloaded from the external OIDC secret.
func TestGetAuthProxySecretValuesOIDCPreservesCookieSecret(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	oidcSecretName := testOIDCSecretName
	oidcSecretValue := "new-oidc-secret-value" //nolint:gosec // test fixture
	existingCookieSecret := "preserved-cookie-value"

	existingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      KubeAuthProxySecretsName,
			Namespace: GatewayNamespace,
		},
		Data: map[string][]byte{
			EnvClientID:     []byte("old-client"),
			EnvClientSecret: []byte("old-secret"),
			EnvCookieSecret: []byte(existingCookieSecret),
		},
	}

	externalSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      oidcSecretName,
			Namespace: GatewayNamespace,
		},
		Data: map[string][]byte{
			"clientSecret": []byte(oidcSecretValue),
		},
	}

	cli := setupTestClient().WithObjects(existingSecret, externalSecret).Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	oidcConfig := &serviceApi.OIDCConfig{
		ClientID: "my-oidc-client",
		ClientSecretRef: corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: oidcSecretName},
			Key:                  "clientSecret",
		},
	}

	clientID, clientSecret, cookieSecret, err := getAuthProxySecretValues(ctx, rr, cluster.AuthModeOIDC, oidcConfig)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(clientID).To(Equal("my-oidc-client"))
	g.Expect(clientSecret).To(Equal(oidcSecretValue), "should reload client secret from OIDC secret")
	g.Expect(cookieSecret).To(Equal(existingCookieSecret), "should preserve existing cookie secret")
}

// TestGetAuthProxySecretValuesUnsupportedAuthMode verifies that an unsupported
// auth mode (e.g. AuthModeNone) returns an error.
func TestGetAuthProxySecretValuesUnsupportedAuthMode(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	cli := setupTestClient().Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	//nolint:dogsled // only the error matters in this test
	_, _, _, err := getAuthProxySecretValues(ctx, rr, cluster.AuthModeNone, nil)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("is not supported"))
}

// TestGetAuthProxySecretValuesIntegratedOAuthPartialSecret verifies that when
// the existing secret is missing the cookie secret key, getAuthProxySecretValues
// falls through to generate a new cookie secret instead of returning incomplete data.
func TestGetAuthProxySecretValuesIntegratedOAuthPartialSecret(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	existingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      KubeAuthProxySecretsName,
			Namespace: GatewayNamespace,
		},
		Data: map[string][]byte{
			EnvClientID:     []byte(AuthClientID),
			EnvClientSecret: []byte(testAuthClientSecret),
		},
	}

	cli := setupTestClient().WithObjects(existingSecret).Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	clientID, clientSecret, cookieSecret, err := getAuthProxySecretValues(ctx, rr, cluster.AuthModeIntegratedOAuth, nil)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(clientID).To(Equal(AuthClientID))
	g.Expect(clientSecret).NotTo(BeEmpty(), "should generate a new client secret when cookie secret is missing")
	g.Expect(cookieSecret).NotTo(BeEmpty(), "should generate a new cookie secret")
}

// TestGetAuthProxySecretValuesOIDCNoExistingSecret verifies that in OIDC mode
// without an existing kube-auth-proxy-creds secret, a new cookie secret is
// generated while the client secret comes from the external OIDC secret.
func TestGetAuthProxySecretValuesOIDCNoExistingSecret(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	oidcSecretName := testOIDCSecretName
	oidcSecretValue := "oidc-secret-value" //nolint:gosec // test fixture

	externalSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      oidcSecretName,
			Namespace: GatewayNamespace,
		},
		Data: map[string][]byte{
			"clientSecret": []byte(oidcSecretValue),
		},
	}

	cli := setupTestClient().WithObjects(externalSecret).Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	oidcConfig := &serviceApi.OIDCConfig{
		ClientID: "fresh-oidc-client",
		ClientSecretRef: corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: oidcSecretName},
			Key:                  "clientSecret",
		},
	}

	clientID, clientSecret, cookieSecret, err := getAuthProxySecretValues(ctx, rr, cluster.AuthModeOIDC, oidcConfig)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(clientID).To(Equal("fresh-oidc-client"))
	g.Expect(clientSecret).To(Equal(oidcSecretValue))
	g.Expect(cookieSecret).NotTo(BeEmpty(), "should generate a new cookie secret when no existing secret")
}

// TestGetAuthProxySecretValuesOIDCMissingKey verifies that when the external
// OIDC secret exists but is missing the expected key, an error is returned.
func TestGetAuthProxySecretValuesOIDCMissingKey(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	oidcSecretName := testOIDCSecretName

	externalSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      oidcSecretName,
			Namespace: GatewayNamespace,
		},
		Data: map[string][]byte{
			"wrong-key": []byte("some-value"),
		},
	}

	cli := setupTestClient().WithObjects(externalSecret).Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	oidcConfig := &serviceApi.OIDCConfig{
		ClientID: "my-client",
		ClientSecretRef: corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: oidcSecretName},
			Key:                  "clientSecret",
		},
	}

	//nolint:dogsled // only the error matters in this test
	_, _, _, err := getAuthProxySecretValues(ctx, rr, cluster.AuthModeOIDC, oidcConfig)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("key 'clientSecret' not found"))
}

// TestDeleteLegacyOAuthClientMalformedURI verifies that deleteLegacyOAuthClient
// skips malformed redirect URIs without erroring, and does not delete the client
// when no valid URIs match.
func TestDeleteLegacyOAuthClientMalformedURI(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	gatewayConfig := &serviceApi.GatewayConfig{
		Spec: serviceApi.GatewayConfigSpec{
			Domain: testDomain,
		},
	}

	legacyClient := &oauthv1.OAuthClient{
		ObjectMeta: metav1.ObjectMeta{
			Name: LegacyAuthClientID,
		},
		RedirectURIs: []string{
			"https://host/%zz",
		},
	}

	cli := setupTestClient().WithObjects(legacyClient).Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	err := deleteLegacyOAuthClient(ctx, rr, gatewayConfig)
	g.Expect(err).NotTo(HaveOccurred())

	preserved := &oauthv1.OAuthClient{}
	err = cli.Get(ctx, types.NamespacedName{Name: LegacyAuthClientID}, preserved)
	g.Expect(err).NotTo(HaveOccurred(), "client with only malformed URIs must not be deleted")
}

// TestDeleteLegacyOAuthClientNonStandardPort verifies that deleteLegacyOAuthClient
// does NOT delete an OAuthClient whose redirect URI uses a non-standard port,
// even if the hostname and path match.
func TestDeleteLegacyOAuthClientNonStandardPort(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	gatewayConfig := &serviceApi.GatewayConfig{
		Spec: serviceApi.GatewayConfigSpec{
			Domain: testDomain,
		},
	}

	legacyClient := &oauthv1.OAuthClient{
		ObjectMeta: metav1.ObjectMeta{
			Name: LegacyAuthClientID,
		},
		RedirectURIs: []string{
			"https://" + testHostnameDefault + ":8443/oauth2/callback",
		},
	}

	cli := setupTestClient().WithObjects(legacyClient).Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	err := deleteLegacyOAuthClient(ctx, rr, gatewayConfig)
	g.Expect(err).NotTo(HaveOccurred())

	preserved := &oauthv1.OAuthClient{}
	err = cli.Get(ctx, types.NamespacedName{Name: LegacyAuthClientID}, preserved)
	g.Expect(err).NotTo(HaveOccurred(), "OAuthClient with non-standard port must not be deleted")
}

// TestDeleteLegacyOAuthClientHTTPSchemeSkipped verifies that deleteLegacyOAuthClient
// does NOT delete an OAuthClient whose redirect URI uses http instead of https.
func TestDeleteLegacyOAuthClientHTTPSchemeSkipped(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	gatewayConfig := &serviceApi.GatewayConfig{
		Spec: serviceApi.GatewayConfigSpec{
			Domain: testDomain,
		},
	}

	legacyClient := &oauthv1.OAuthClient{
		ObjectMeta: metav1.ObjectMeta{
			Name: LegacyAuthClientID,
		},
		RedirectURIs: []string{
			"http://" + testHostnameDefault + "/oauth2/callback",
		},
	}

	cli := setupTestClient().WithObjects(legacyClient).Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	err := deleteLegacyOAuthClient(ctx, rr, gatewayConfig)
	g.Expect(err).NotTo(HaveOccurred())

	preserved := &oauthv1.OAuthClient{}
	err = cli.Get(ctx, types.NamespacedName{Name: LegacyAuthClientID}, preserved)
	g.Expect(err).NotTo(HaveOccurred(), "OAuthClient with http scheme must not be deleted")
}

// TestDeleteLegacyOAuthClientExplicitPort443 verifies that deleteLegacyOAuthClient
// deletes the legacy OAuthClient when its redirect URI uses explicit port 443.
func TestDeleteLegacyOAuthClientExplicitPort443(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	gatewayConfig := &serviceApi.GatewayConfig{
		Spec: serviceApi.GatewayConfigSpec{
			Domain: testDomain,
		},
	}

	legacyClient := &oauthv1.OAuthClient{
		ObjectMeta: metav1.ObjectMeta{
			Name: LegacyAuthClientID,
		},
		RedirectURIs: []string{
			"https://" + testHostnameDefault + ":443/oauth2/callback",
		},
	}

	cli := setupTestClient().WithObjects(legacyClient).Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	err := deleteLegacyOAuthClient(ctx, rr, gatewayConfig)
	g.Expect(err).NotTo(HaveOccurred())

	deleted := &oauthv1.OAuthClient{}
	err = cli.Get(ctx, types.NamespacedName{Name: LegacyAuthClientID}, deleted)
	g.Expect(k8serr.IsNotFound(err)).To(BeTrue(), "legacy OAuthClient with explicit port 443 should be deleted")
}

// TestDeleteLegacyOAuthClientGetFQDNError verifies that deleteLegacyOAuthClient
// returns an error when GetFQDN fails (e.g. no domain in config and no cluster domain).
func TestDeleteLegacyOAuthClientGetFQDNError(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	gatewayConfig := &serviceApi.GatewayConfig{
		Spec: serviceApi.GatewayConfigSpec{},
	}

	legacyClient := &oauthv1.OAuthClient{
		ObjectMeta: metav1.ObjectMeta{
			Name: LegacyAuthClientID,
		},
		RedirectURIs: []string{
			"https://rh-ai.example.com/oauth2/callback",
		},
	}

	cli := setupTestClient().WithObjects(legacyClient).Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	err := deleteLegacyOAuthClient(ctx, rr, gatewayConfig)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("failed to resolve gateway domain for legacy cleanup"))
}

// TestGetAuthProxySecretValuesOIDCMissingExternalSecret verifies that when the
// external OIDC secret referenced by ClientSecretRef does not exist, an error
// is returned.
func TestGetAuthProxySecretValuesOIDCMissingExternalSecret(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	cli := setupTestClient().Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	oidcConfig := &serviceApi.OIDCConfig{
		ClientID: "my-oidc-client",
		ClientSecretRef: corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "nonexistent-secret"},
			Key:                  "clientSecret",
		},
	}

	//nolint:dogsled // only the error matters in this test
	_, _, _, err := getAuthProxySecretValues(ctx, rr, cluster.AuthModeOIDC, oidcConfig)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("failed to get OIDC client secret"))
}

// TestGetAuthProxySecretValuesOIDCDefaultKey verifies that when the OIDC
// ClientSecretRef.Key is empty, it defaults to "clientSecret".
func TestGetAuthProxySecretValuesOIDCDefaultKey(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	oidcSecretName := testOIDCSecretName
	oidcSecretValue := "oidc-default-key-value" //nolint:gosec // test fixture

	externalSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      oidcSecretName,
			Namespace: GatewayNamespace,
		},
		Data: map[string][]byte{
			"clientSecret": []byte(oidcSecretValue),
		},
	}

	cli := setupTestClient().WithObjects(externalSecret).Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	oidcConfig := &serviceApi.OIDCConfig{
		ClientID: "oidc-client-default-key",
		ClientSecretRef: corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: oidcSecretName},
		},
	}

	clientID, clientSecret, cookieSecret, err := getAuthProxySecretValues(ctx, rr, cluster.AuthModeOIDC, oidcConfig)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(clientID).To(Equal("oidc-client-default-key"))
	g.Expect(clientSecret).To(Equal(oidcSecretValue), "should read from default 'clientSecret' key")
	g.Expect(cookieSecret).NotTo(BeEmpty(), "should generate a cookie secret")
}

// TestGetAuthProxySecretValuesOIDCCustomNamespace verifies that when OIDC
// SecretNamespace is set, the external secret is fetched from that namespace.
func TestGetAuthProxySecretValuesOIDCCustomNamespace(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	customNamespace := "custom-ns"
	oidcSecretName := testOIDCSecretName
	oidcSecretValue := "oidc-custom-ns-value" //nolint:gosec // test fixture

	externalSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      oidcSecretName,
			Namespace: customNamespace,
		},
		Data: map[string][]byte{
			"my-key": []byte(oidcSecretValue),
		},
	}

	cli := setupTestClient().WithObjects(externalSecret).Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	oidcConfig := &serviceApi.OIDCConfig{
		ClientID:        "oidc-custom-ns-client",
		SecretNamespace: customNamespace,
		ClientSecretRef: corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: oidcSecretName},
			Key:                  "my-key",
		},
	}

	clientID, clientSecret, cookieSecret, err := getAuthProxySecretValues(ctx, rr, cluster.AuthModeOIDC, oidcConfig)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(clientID).To(Equal("oidc-custom-ns-client"))
	g.Expect(clientSecret).To(Equal(oidcSecretValue), "should read from custom namespace")
	g.Expect(cookieSecret).NotTo(BeEmpty(), "should generate a cookie secret")
}

// TestGetAuthProxySecretValuesOIDCExistingSecretMissingCookie verifies that in
// OIDC mode, when the existing secret has a client secret but no cookie secret,
// a new cookie secret is generated while the client secret comes from the
// external OIDC secret.
func TestGetAuthProxySecretValuesOIDCExistingSecretMissingCookie(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	oidcSecretName := testOIDCSecretName
	oidcSecretValue := "oidc-secret-no-cookie" //nolint:gosec // test fixture

	existingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      KubeAuthProxySecretsName,
			Namespace: GatewayNamespace,
		},
		Data: map[string][]byte{
			EnvClientID:     []byte("old-oidc-client"),
			EnvClientSecret: []byte("old-secret"),
		},
	}

	externalSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      oidcSecretName,
			Namespace: GatewayNamespace,
		},
		Data: map[string][]byte{
			"clientSecret": []byte(oidcSecretValue),
		},
	}

	cli := setupTestClient().WithObjects(existingSecret, externalSecret).Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	oidcConfig := &serviceApi.OIDCConfig{
		ClientID: "oidc-no-cookie-client",
		ClientSecretRef: corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: oidcSecretName},
			Key:                  "clientSecret",
		},
	}

	clientID, clientSecret, cookieSecret, err := getAuthProxySecretValues(ctx, rr, cluster.AuthModeOIDC, oidcConfig)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(clientID).To(Equal("oidc-no-cookie-client"))
	g.Expect(clientSecret).To(Equal(oidcSecretValue), "should reload from OIDC secret")
	g.Expect(cookieSecret).NotTo(BeEmpty(), "should generate a new cookie secret when missing from existing secret")
}

// TestGetAuthProxySecretValuesIntegratedOAuthMissingClientSecret verifies that
// when the existing secret has a cookie secret but no client secret, new secrets
// are generated while the correct client ID is returned.
func TestGetAuthProxySecretValuesIntegratedOAuthMissingClientSecret(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	existingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      KubeAuthProxySecretsName,
			Namespace: GatewayNamespace,
		},
		Data: map[string][]byte{
			EnvClientID:     []byte(AuthClientID),
			EnvCookieSecret: []byte(testAuthCookieSecret),
		},
	}

	cli := setupTestClient().WithObjects(existingSecret).Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	clientID, clientSecret, cookieSecret, err := getAuthProxySecretValues(ctx, rr, cluster.AuthModeIntegratedOAuth, nil)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(clientID).To(Equal(AuthClientID))
	g.Expect(clientSecret).NotTo(BeEmpty(), "should generate a new client secret")
	g.Expect(clientSecret).NotTo(Equal(testAuthClientSecret), "should not reuse missing client secret")
	g.Expect(cookieSecret).To(Equal(testAuthCookieSecret), "should preserve existing cookie secret")
}

// TestDeleteLegacyOAuthClientGetError verifies that when the API server returns
// a non-NotFound error while checking for the legacy OAuthClient, the error is
// propagated to the caller.
func TestDeleteLegacyOAuthClientGetError(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	gatewayConfig := &serviceApi.GatewayConfig{
		Spec: serviceApi.GatewayConfigSpec{
			Domain: testDomain,
		},
	}

	cli := setupTestClient().
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if key.Name == LegacyAuthClientID {
					return fmt.Errorf("simulated API server error")
				}
				return c.Get(ctx, key, obj, opts...)
			},
		}).
		Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	err := deleteLegacyOAuthClient(ctx, rr, gatewayConfig)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("failed to check for legacy OAuthClient"))
}

// TestDeleteLegacyOAuthClientDeleteError verifies that when deletion of the
// legacy OAuthClient fails with a non-transient error, the error is propagated.
func TestDeleteLegacyOAuthClientDeleteError(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	gatewayConfig := &serviceApi.GatewayConfig{
		Spec: serviceApi.GatewayConfigSpec{
			Domain: testDomain,
		},
	}

	legacyClient := &oauthv1.OAuthClient{
		ObjectMeta: metav1.ObjectMeta{
			Name: LegacyAuthClientID,
		},
		RedirectURIs: []string{
			"https://" + testHostnameDefault + "/oauth2/callback",
		},
	}

	cli := setupTestClient().
		WithObjects(legacyClient).
		WithInterceptorFuncs(interceptor.Funcs{
			Delete: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				if obj.GetName() == LegacyAuthClientID {
					return fmt.Errorf("simulated delete error")
				}
				return c.Delete(ctx, obj, opts...)
			},
		}).
		Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli}
	ctx := t.Context()

	err := deleteLegacyOAuthClient(ctx, rr, gatewayConfig)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("failed to delete legacy OAuthClient"))
}
