//nolint:testpackage
package gateway

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"

	. "github.com/onsi/gomega"
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
					Name: "test-gateway",
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
					Name: "test-gateway",
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
					Name: "test-gateway",
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
					Name: "test-gateway",
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
			expectedTimeout: "5s",
			description:     "should return default 5s when gatewayConfig is nil",
		},
		{
			name: "returns default when no timeout specified",
			gatewayConfig: &serviceApi.GatewayConfig{
				Spec: serviceApi.GatewayConfigSpec{},
			},
			expectedTimeout: "5s",
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
			expectedTimeout: "5s",
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
			expectedTimeout: "5s",
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
			"OAUTH2_PROXY_CLIENT_ID":     []byte("client-id"),
			"OAUTH2_PROXY_CLIENT_SECRET": []byte("client-secret"),
			"OAUTH2_PROXY_COOKIE_SECRET": []byte("cookie-secret"),
		},
	}
	hash1 := calculateAuthConfigHash(secret1)

	// Change client ID
	secret2 := &corev1.Secret{
		Data: map[string][]byte{
			"OAUTH2_PROXY_CLIENT_ID":     []byte("different-client-id"),
			"OAUTH2_PROXY_CLIENT_SECRET": []byte("client-secret"),
			"OAUTH2_PROXY_COOKIE_SECRET": []byte("cookie-secret"),
		},
	}
	hash2 := calculateAuthConfigHash(secret2)
	g.Expect(hash2).NotTo(Equal(hash1), "hash should change when client ID changes")

	// Change client secret
	secret3 := &corev1.Secret{
		Data: map[string][]byte{
			"OAUTH2_PROXY_CLIENT_ID":     []byte("client-id"),
			"OAUTH2_PROXY_CLIENT_SECRET": []byte("different-client-secret"),
			"OAUTH2_PROXY_COOKIE_SECRET": []byte("cookie-secret"),
		},
	}
	hash3 := calculateAuthConfigHash(secret3)
	g.Expect(hash3).NotTo(Equal(hash1), "hash should change when client secret changes")

	// Change cookie secret
	secret4 := &corev1.Secret{
		Data: map[string][]byte{
			"OAUTH2_PROXY_CLIENT_ID":     []byte("client-id"),
			"OAUTH2_PROXY_CLIENT_SECRET": []byte("client-secret"),
			"OAUTH2_PROXY_COOKIE_SECRET": []byte("different-cookie-secret"),
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
			"OAUTH2_PROXY_CLIENT_ID":     []byte("client-id"),
			"OAUTH2_PROXY_CLIENT_SECRET": []byte("client-secret"),
			"OAUTH2_PROXY_COOKIE_SECRET": []byte("cookie-secret"),
		},
	}
	hash5 := calculateAuthConfigHash(secret5)
	g.Expect(hash5).To(Equal(hash1), "same secret values should produce same hash")
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
					Domain: "apps.example.com",
				},
			},
			expectedDomain: "data-science-gateway.apps.example.com",
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
					Domain:    "apps.example.com",
					Subdomain: "my-gateway",
				},
			},
			expectedDomain: "my-gateway.apps.example.com",
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
					Domain:    "apps.example.com",
					Subdomain: "   ",
				},
			},
			expectedDomain: "data-science-gateway.apps.example.com",
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
			expectedExpire:  "24h0m0s",
			expectedRefresh: "1h0m0s",
			description:     "should return 24h and 1h when cookieConfig is nil",
		},
		{
			name: "returns defaults when both durations are zero",
			cookieConfig: &serviceApi.CookieConfig{
				Expire:  metav1.Duration{Duration: 0},
				Refresh: metav1.Duration{Duration: 0},
			},
			expectedExpire:  "24h0m0s",
			expectedRefresh: "1h0m0s",
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
			expectedRefresh: "1h0m0s",
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
