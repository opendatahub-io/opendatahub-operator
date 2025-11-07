//nolint:testpackage
package gateway

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

	. "github.com/onsi/gomega"
)

const (
	// Test constants for gateway support functions.
	testCertSecretSupport = "test-cert-secret"
	testDomainSupport     = "data-science-gateway.apps.test-cluster.com"
	testClusterDomain     = "apps.cluster.example.com"
	testUserDomain        = "apps.example.com"
	customCertSecret      = "my-cert"

	// Expected values constants.
	expectedHTTPSListenerName    = "https"
	expectedHTTPSPortSupport     = gwapiv1.PortNumber(443)
	expectedHTTPSProtocolSupport = gwapiv1.HTTPSProtocolType
	expectedODHDomain            = "data-science-gateway.apps.example.com"
	expectedClusterDomain        = "data-science-gateway.apps.cluster.example.com"
)

// TestCreateListeners tests the createListeners helper function.
func TestCreateListeners(t *testing.T) {
	t.Parallel()
	g := NewWithT(t) // Create once outside the loop for better performance

	testCases := []struct {
		name        string
		certSecret  string
		domain      string
		expectCount int
		description string
	}{
		{
			name:        "creates HTTPS listener when certificate provided",
			certSecret:  testCertSecretSupport,
			domain:      testDomainSupport,
			expectCount: 1,
			description: "should create one HTTPS listener with certificate",
		},
		{
			name:        "creates no listeners when no certificate",
			certSecret:  "",
			domain:      testDomainSupport,
			expectCount: 0,
			description: "should create no listeners without certificate",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			listeners := createListeners(tc.certSecret, tc.domain)

			g.Expect(listeners).To(HaveLen(tc.expectCount), tc.description)

			if tc.expectCount > 0 {
				listener := listeners[0]
				g.Expect(string(listener.Name)).To(Equal(expectedHTTPSListenerName))
				g.Expect(listener.Protocol).To(Equal(expectedHTTPSProtocolSupport))
				g.Expect(listener.Port).To(Equal(expectedHTTPSPortSupport))
				g.Expect(listener.Hostname).NotTo(BeNil())
				g.Expect(string(*listener.Hostname)).To(Equal(tc.domain))
				g.Expect(listener.TLS).NotTo(BeNil())
				g.Expect(listener.TLS.CertificateRefs).To(HaveLen(1))
				g.Expect(string(listener.TLS.CertificateRefs[0].Name)).To(Equal(tc.certSecret))
			}
		})
	}
}

// TestGetCertificateType tests the getCertificateType helper function.
func TestGetCertificateType(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	testCases := []struct {
		name            string
		certificateSpec *infrav1.CertificateSpec
		gatewayConfig   *serviceApi.GatewayConfig
		expectedType    string
		description     string
	}{
		{
			name:            "returns default when certificate is nil",
			certificateSpec: nil,
			gatewayConfig:   nil, // Will use createTestGatewayConfigSupport
			expectedType:    string(infrav1.OpenshiftDefaultIngress),
			description:     "should return OpenShift default when no certificate specified",
		},
		{
			name:            "returns default when gatewayConfig is nil",
			certificateSpec: nil,
			gatewayConfig:   nil, // Explicitly nil for this test
			expectedType:    string(infrav1.OpenshiftDefaultIngress),
			description:     "should return OpenShift default when gatewayConfig is nil",
		},
		{
			name: "returns certificate type when specified",
			certificateSpec: &infrav1.CertificateSpec{
				Type: infrav1.SelfSigned,
			},
			expectedType: string(infrav1.SelfSigned),
			description:  "should return the specified certificate type",
		},
		{
			name: "returns provided certificate type",
			certificateSpec: &infrav1.CertificateSpec{
				Type:       infrav1.Provided,
				SecretName: customCertSecret,
			},
			expectedType: string(infrav1.Provided),
			description:  "should return provided certificate type",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var gateway *serviceApi.GatewayConfig
			if tc.name == "returns default when gatewayConfig is nil" {
				gateway = nil // Explicitly test nil case
			} else {
				gateway = createTestGatewayConfigSupport(testDomainSupport, tc.certificateSpec)
			}

			certType := getCertificateType(gateway)
			g.Expect(certType).To(Equal(tc.expectedType), tc.description)
		})
	}
}

// TestResolveDomain tests the resolveDomain helper function.
func TestResolveDomain(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	testCases := []struct {
		name              string
		specDomain        string
		clusterDomain     string
		useClusterIngress bool
		expectedDomain    string
		expectError       bool
		description       string
	}{
		{
			name:              "user provided domain",
			specDomain:        testUserDomain,
			useClusterIngress: false,
			expectedDomain:    expectedODHDomain,
			expectError:       false,
			description:       "should use user-provided domain and prepend gateway name",
		},
		{
			name:              "empty domain falls back to cluster domain",
			specDomain:        "",
			clusterDomain:     testClusterDomain,
			useClusterIngress: true,
			expectedDomain:    expectedClusterDomain,
			expectError:       false,
			description:       "should fall back to cluster domain when spec domain is empty",
		},
		{
			name:              "cluster domain retrieval fails",
			specDomain:        "",
			useClusterIngress: false,
			expectedDomain:    "",
			expectError:       true,
			description:       "should return error when cluster domain retrieval fails",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			// Create test gateway config using helper
			gatewayConfig := createTestGatewayConfigSupport(tc.specDomain, nil)

			// Setup client with or without cluster ingress using helpers
			var client client.Client
			if tc.useClusterIngress && tc.clusterDomain != "" {
				client = setupSupportTestClientWithClusterIngress(tc.clusterDomain)
			} else {
				client = setupSupportTestClient()
			}

			domain, err := resolveDomain(ctx, client, gatewayConfig)

			if tc.expectError {
				g.Expect(err).To(HaveOccurred(), tc.description)
				g.Expect(domain).To(Equal(""), "domain should be empty on error")
			} else {
				g.Expect(err).ToNot(HaveOccurred(), tc.description)
				g.Expect(domain).To(Equal(tc.expectedDomain), tc.description)
			}
		})
	}
}

// TestCreateListenersEdgeCases tests edge cases for the createListeners function.
func TestCreateListenersEdgeCases(t *testing.T) {
	t.Parallel()
	g := NewWithT(t) // Create once outside the loop for better performance

	testCases := []struct {
		name        string
		certSecret  string
		domain      string
		expectCount int
		description string
	}{
		{
			name:        "whitespace-only certificate secret",
			certSecret:  "   ",
			domain:      testDomainSupport,
			expectCount: 1, // Whitespace is treated as valid certificate name
			description: "should create listener with whitespace certificate name",
		},
		{
			name:        "empty domain with certificate",
			certSecret:  testCertSecretSupport,
			domain:      "",
			expectCount: 1, // Empty domain still creates listener
			description: "should create listener even with empty domain",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			listeners := createListeners(tc.certSecret, tc.domain)

			g.Expect(listeners).To(HaveLen(tc.expectCount), tc.description)

			if tc.expectCount > 0 {
				listener := listeners[0]
				g.Expect(string(listener.Name)).To(Equal(expectedHTTPSListenerName))
				g.Expect(listener.Protocol).To(Equal(expectedHTTPSProtocolSupport))
				g.Expect(listener.Port).To(Equal(expectedHTTPSPortSupport))

				// Hostname should be present even if domain is empty
				g.Expect(listener.Hostname).NotTo(BeNil())
				g.Expect(string(*listener.Hostname)).To(Equal(tc.domain))
			}
		})
	}
}

// TestResolveDomainNilHandling tests nil parameter handling for resolveDomain.
func TestResolveDomainNilHandling(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := t.Context()
	client := setupSupportTestClient()

	// Test nil gatewayConfig - should fall back to cluster domain (which will fail with our test client)
	domain, err := resolveDomain(ctx, client, nil)
	g.Expect(err).To(HaveOccurred(), "should return error when gatewayConfig is nil and cluster domain fails")
	g.Expect(domain).To(Equal(""), "domain should be empty on error")
}

// TestResolveDomainEdgeCases tests additional edge cases for domain resolution.
func TestResolveDomainEdgeCases(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	testCases := []struct {
		name             string
		specDomain       string
		gatewayName      string
		expectedContains string
		description      string
	}{
		{
			name:             "user domain with default gateway name",
			specDomain:       testUserDomain,
			gatewayName:      DefaultGatewayName,
			expectedContains: DefaultGatewayName,
			description:      "should use default gateway name with user domain",
		},
		{
			name:             "domain with subdomain",
			specDomain:       "api.v1.apps.example.com",
			gatewayName:      DefaultGatewayName,
			expectedContains: "data-science-gateway.api.v1.apps.example.com",
			description:      "should handle complex subdomain structures",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			gatewayConfig := createTestGatewayConfigSupport(tc.specDomain, nil)
			client := setupSupportTestClient()

			domain, err := resolveDomain(ctx, client, gatewayConfig)

			g.Expect(err).ToNot(HaveOccurred(), tc.description)
			g.Expect(domain).To(ContainSubstring(tc.expectedContains), tc.description)
		})
	}
}

// TestResolveDomainWithSubdomain tests subdomain functionality in domain resolution.
func TestResolveDomainWithSubdomain(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	testCases := []struct {
		name              string
		specDomain        string
		subdomain         string
		clusterDomain     string
		useClusterIngress bool
		expectedDomain    string
		expectError       bool
		description       string
	}{
		{
			name:              "custom subdomain with user domain",
			specDomain:        testUserDomain,
			subdomain:         "my-gateway",
			useClusterIngress: false,
			expectedDomain:    "my-gateway.apps.example.com",
			expectError:       false,
			description:       "should use custom subdomain with user-provided domain",
		},
		{
			name:              "custom subdomain with cluster domain",
			specDomain:        "",
			subdomain:         "custom-gateway",
			clusterDomain:     testClusterDomain,
			useClusterIngress: true,
			expectedDomain:    "custom-gateway.apps.cluster.example.com",
			expectError:       false,
			description:       "should use custom subdomain with cluster domain",
		},
		{
			name:              "empty subdomain falls back to default",
			specDomain:        testUserDomain,
			subdomain:         "",
			useClusterIngress: false,
			expectedDomain:    expectedODHDomain,
			expectError:       false,
			description:       "should fall back to default gateway name when subdomain is empty",
		},
		{
			name:              "whitespace subdomain falls back to default",
			specDomain:        testUserDomain,
			subdomain:         "   ",
			useClusterIngress: false,
			expectedDomain:    expectedODHDomain,
			expectError:       false,
			description:       "should fall back to default gateway name when subdomain is whitespace",
		},
		{
			name:              "subdomain with complex domain",
			specDomain:        "api.v1.example.com",
			subdomain:         "data-science",
			useClusterIngress: false,
			expectedDomain:    "data-science.api.v1.example.com",
			expectError:       false,
			description:       "should use subdomain with complex domain structure",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()

			// Create test gateway config with subdomain
			gatewayConfig := createTestGatewayConfigSupportWithSubdomain(tc.specDomain, tc.subdomain, nil)

			// Setup client with or without cluster ingress
			var client client.Client
			if tc.useClusterIngress && tc.clusterDomain != "" {
				client = setupSupportTestClientWithClusterIngress(tc.clusterDomain)
			} else {
				client = setupSupportTestClient()
			}

			domain, err := resolveDomain(ctx, client, gatewayConfig)

			if tc.expectError {
				g.Expect(err).To(HaveOccurred(), tc.description)
				g.Expect(domain).To(Equal(""), "domain should be empty on error")
			} else {
				g.Expect(err).ToNot(HaveOccurred(), tc.description)
				g.Expect(domain).To(Equal(tc.expectedDomain), tc.description)
			}
		})
	}
}

// TestBuildGatewayDomainWithSubdomain tests the buildGatewayDomain function with subdomain.
func TestBuildGatewayDomainWithSubdomain(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	testCases := []struct {
		name        string
		subdomain   string
		baseDomain  string
		expected    string
		description string
	}{
		{
			name:        "custom subdomain",
			subdomain:   "my-gateway",
			baseDomain:  "apps.example.com",
			expected:    "my-gateway.apps.example.com",
			description: "should use custom subdomain when provided",
		},
		{
			name:        "empty subdomain uses default",
			subdomain:   "",
			baseDomain:  "apps.example.com",
			expected:    "data-science-gateway.apps.example.com",
			description: "should use default gateway name when subdomain is empty",
		},
		{
			name:        "whitespace subdomain uses default",
			subdomain:   "   ",
			baseDomain:  "apps.example.com",
			expected:    "data-science-gateway.apps.example.com",
			description: "should use default gateway name when subdomain is whitespace",
		},
		{
			name:        "subdomain with cluster domain",
			subdomain:   "custom",
			baseDomain:  testClusterDomain,
			expected:    "custom.apps.cluster.example.com",
			description: "should use subdomain with cluster domain",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := buildGatewayDomain(tc.subdomain, tc.baseDomain)
			g.Expect(result).To(Equal(tc.expected), tc.description)
		})
	}
}

// TestValidateOIDCConfig tests the OIDC configuration validation logic.
func TestValidateOIDCConfig(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		authMode    AuthMode
		oidcConfig  *serviceApi.OIDCConfig
		expectError bool
		description string
	}{
		{
			name:     "OIDC mode with valid config",
			authMode: AuthModeOIDC,
			oidcConfig: &serviceApi.OIDCConfig{
				IssuerURL: testOIDCIssuerURL,
				ClientID:  "test-client",
				ClientSecretRef: corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
				},
			},
			expectError: false,
			description: "should pass when OIDC mode has valid configuration",
		},
		{
			name:        "OIDC mode with missing config",
			authMode:    AuthModeOIDC,
			oidcConfig:  nil,
			expectError: true,
			description: "should fail when OIDC mode has no configuration",
		},
		{
			name:     "OIDC mode with empty clientID",
			authMode: AuthModeOIDC,
			oidcConfig: &serviceApi.OIDCConfig{
				IssuerURL: testOIDCIssuerURL,
				ClientID:  "",
				ClientSecretRef: corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
				},
			},
			expectError: true,
			description: "should fail when clientID is empty",
		},
		{
			name:     "OIDC mode with all fields empty",
			authMode: AuthModeOIDC,
			oidcConfig: &serviceApi.OIDCConfig{
				IssuerURL: "",
				ClientID:  "",
				ClientSecretRef: corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: ""},
				},
			},
			expectError: true,
			description: "should fail when all required fields are empty",
		},
		{
			name:        "IntegratedOAuth mode",
			authMode:    AuthModeIntegratedOAuth,
			oidcConfig:  nil,
			expectError: false,
			description: "should pass for IntegratedOAuth mode regardless of OIDC config",
		},
		{
			name:        "None mode",
			authMode:    AuthModeNone,
			oidcConfig:  nil,
			expectError: false,
			description: "should pass for None mode regardless of OIDC config",
		},
	}

	for _, tc := range testCases {
		// capture range var for parallel subtests
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)
			condition := validateOIDCConfig(tc.authMode, tc.oidcConfig)

			if tc.expectError {
				g.Expect(condition).NotTo(BeNil(), tc.description)
				g.Expect(condition.Type).To(Equal(status.ConditionTypeReady))
				g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(condition.Reason).To(Equal(status.NotReadyReason))
				g.Expect(condition.Message).To(ContainSubstring("OIDC"))
			} else {
				g.Expect(condition).To(BeNil(), tc.description)
			}
		})
	}
}

// TestCheckAuthModeNone tests the None authentication mode validation logic.
func TestCheckAuthModeNone(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		authMode    AuthMode
		expectError bool
		description string
	}{
		{
			name:        "None auth mode",
			authMode:    AuthModeNone,
			expectError: true,
			description: "should return condition when auth mode is None",
		},
		{
			name:        "IntegratedOAuth auth mode",
			authMode:    AuthModeIntegratedOAuth,
			expectError: false,
			description: "should return nil for IntegratedOAuth mode",
		},
		{
			name:        "OIDC auth mode",
			authMode:    AuthModeOIDC,
			expectError: false,
			description: "should return nil for OIDC mode",
		},
	}

	for _, tc := range testCases {
		// capture range var for parallel subtests
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)
			condition := checkAuthModeNone(tc.authMode)

			if tc.expectError {
				g.Expect(condition).NotTo(BeNil(), tc.description)
				g.Expect(condition.Type).To(Equal(status.ConditionTypeReady))
				g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(condition.Reason).To(Equal(status.NotReadyReason))
				g.Expect(condition.Message).To(ContainSubstring("external authentication"))
			} else {
				g.Expect(condition).To(BeNil(), tc.description)
			}
		})
	}
}

// TestCreateKubeAuthProxyService tests the auth proxy service creation logic.
func TestCreateKubeAuthProxyService(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	rr := &odhtypes.ReconciliationRequest{
		Client: setupTestClient(),
	}

	err := createKubeAuthProxyService(rr)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rr.Resources).To(HaveLen(1))

	// Convert unstructured to typed Service
	serviceResource := &rr.Resources[0]
	service := &corev1.Service{}
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(serviceResource.Object, service)
	g.Expect(err).NotTo(HaveOccurred())

	// Verify service properties
	g.Expect(service.GetName()).To(Equal(KubeAuthProxyName))
	g.Expect(service.GetNamespace()).To(Equal(GatewayNamespace))
	g.Expect(service.Labels).To(Equal(KubeAuthProxyLabels))
	g.Expect(service.Spec.Selector).To(Equal(KubeAuthProxyLabels))

	// Verify annotations
	expectedAnnotations := map[string]string{
		"service.beta.openshift.io/serving-cert-secret-name": KubeAuthProxyTLSName,
	}
	g.Expect(service.Annotations).To(Equal(expectedAnnotations))

	// Verify port configuration
	g.Expect(service.Spec.Ports).To(HaveLen(2))

	// Verify HTTPS port
	httpsPort := service.Spec.Ports[0]
	g.Expect(httpsPort.Name).To(Equal("https"))
	g.Expect(httpsPort.Port).To(Equal(int32(AuthProxyHTTPSPort)))
	g.Expect(httpsPort.TargetPort).To(Equal(intstr.FromInt(AuthProxyHTTPSPort)))

	// Verify metrics port
	metricsPort := service.Spec.Ports[1]
	g.Expect(metricsPort.Name).To(Equal("metrics"))
	g.Expect(metricsPort.Port).To(Equal(int32(AuthProxyMetricsPort)))
	g.Expect(metricsPort.TargetPort).To(Equal(intstr.FromInt(AuthProxyMetricsPort)))
}

// TestCreateOAuthCallbackRoute tests the OAuth callback route creation logic.
func TestCreateOAuthCallbackRoute(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	rr := &odhtypes.ReconciliationRequest{
		Client: setupTestClient(),
	}

	err := createOAuthCallbackRoute(rr)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rr.Resources).To(HaveLen(1))

	// Convert unstructured to typed HTTPRoute
	routeResource := &rr.Resources[0]
	httpRoute := &gwapiv1.HTTPRoute{}
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(routeResource.Object, httpRoute)
	g.Expect(err).NotTo(HaveOccurred())

	// Verify HTTPRoute properties
	g.Expect(httpRoute.GetName()).To(Equal(OAuthCallbackRouteName))
	g.Expect(httpRoute.GetNamespace()).To(Equal(GatewayNamespace))

	// Verify ParentRefs (gateway reference)
	g.Expect(httpRoute.Spec.ParentRefs).To(HaveLen(1))
	parentRef := httpRoute.Spec.ParentRefs[0]
	expectedGatewayName := DefaultGatewayName
	g.Expect(string(parentRef.Name)).To(Equal(expectedGatewayName))
	g.Expect(string(*parentRef.Namespace)).To(Equal(GatewayNamespace))

	// Verify routing rules
	g.Expect(httpRoute.Spec.Rules).To(HaveLen(1))
	rule := httpRoute.Spec.Rules[0]

	// Verify path matching
	g.Expect(rule.Matches).To(HaveLen(1))
	match := rule.Matches[0]
	g.Expect(match.Path).NotTo(BeNil())
	g.Expect(*match.Path.Value).To(Equal(AuthProxyOAuth2Path))
	g.Expect(*match.Path.Type).To(Equal(gwapiv1.PathMatchPathPrefix))

	// Verify backend refs
	g.Expect(rule.BackendRefs).To(HaveLen(1))
	backendRef := rule.BackendRefs[0]
	g.Expect(string(backendRef.Name)).To(Equal(KubeAuthProxyName))
	g.Expect(*backendRef.Port).To(Equal(gwapiv1.PortNumber(AuthProxyHTTPSPort)))
}

// TestBuildOAuth2ProxyArgsOpenShift tests OAuth2 proxy arguments for OpenShift mode.
func TestBuildOAuth2ProxyArgsOpenShift(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	args := buildOAuth2ProxyArgs(nil, nil, expectedODHDomain) // nil OIDC = OpenShift mode, nil cookie = defaults

	// Verify base arguments are present
	g.Expect(args).To(ContainElement(ContainSubstring("--http-address=0.0.0.0:")))
	g.Expect(args).To(ContainElement("--email-domain=*"))
	g.Expect(args).To(ContainElement("--upstream=static://200"))
	g.Expect(args).To(ContainElement("--skip-provider-button"))
	g.Expect(args).To(ContainElement("--pass-access-token=true"))
	g.Expect(args).To(ContainElement("--set-xauthrequest=true"))
	g.Expect(args).To(ContainElement(ContainSubstring("--redirect-url=https://")))

	// Verify OpenShift-specific arguments
	g.Expect(args).To(ContainElement("--provider=openshift"))
	g.Expect(args).To(ContainElement("--scope=" + OpenShiftOAuthScope))
	g.Expect(args).To(ContainElement(ContainSubstring("--tls-cert-file=" + TLSCertsMountPath)))
	g.Expect(args).To(ContainElement(ContainSubstring("--tls-key-file=" + TLSCertsMountPath)))
	g.Expect(args).To(ContainElement(ContainSubstring("--https-address=0.0.0.0:")))

	// Verify OIDC-specific arguments are NOT present
	g.Expect(args).NotTo(ContainElement("--provider=oidc"))
	g.Expect(args).NotTo(ContainElement(ContainSubstring("--oidc-issuer-url=")))
}

// TestBuildOAuth2ProxyArgsOIDC tests OAuth2 proxy arguments for OIDC mode.
func TestBuildOAuth2ProxyArgsOIDC(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	oidcConfig := &serviceApi.OIDCConfig{
		IssuerURL: testOIDCIssuerURL,
	}

	args := buildOAuth2ProxyArgs(oidcConfig, nil, expectedODHDomain) // nil cookie = defaults

	// Verify base arguments are present
	g.Expect(args).To(ContainElement(ContainSubstring("--http-address=0.0.0.0:")))
	g.Expect(args).To(ContainElement("--email-domain=*"))
	g.Expect(args).To(ContainElement("--upstream=static://200"))
	g.Expect(args).To(ContainElement("--skip-provider-button"))
	g.Expect(args).To(ContainElement("--pass-access-token=true"))
	g.Expect(args).To(ContainElement("--set-xauthrequest=true"))
	g.Expect(args).To(ContainElement(ContainSubstring("--redirect-url=https://")))

	// Verify OIDC-specific arguments
	g.Expect(args).To(ContainElement("--provider=oidc"))
	g.Expect(args).To(ContainElement("--oidc-issuer-url=" + testOIDCIssuerURL))

	// Verify OpenShift-specific arguments are NOT present
	g.Expect(args).NotTo(ContainElement("--provider=openshift"))
	g.Expect(args).NotTo(ContainElement("--scope=" + OpenShiftOAuthScope))

	// Verify OIDC-specific HTTPS arguments ARE present (required for EnvoyFilter integration)
	g.Expect(args).To(ContainElement(ContainSubstring("--tls-cert-file=")))
	g.Expect(args).To(ContainElement(ContainSubstring("--https-address=")))
	g.Expect(args).To(ContainElement("--use-system-trust-store=true"))
}

// TestCreateDashboardRoute and TestCreateReferenceGrant removed
// Dashboard routing is now user's responsibility

// TestGetOIDCClientSecret tests OIDC client secret retrieval logic.
func TestGetOIDCClientSecret(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		oidcConfig  *serviceApi.OIDCConfig
		secretData  map[string][]byte
		expectedVal string
		expectError bool
		description string
	}{
		{
			name: "successful retrieval with default key",
			oidcConfig: &serviceApi.OIDCConfig{
				ClientSecretRef: corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "oidc-secret",
					},
				},
			},
			secretData: map[string][]byte{
				DefaultClientSecretKey: []byte("secret-value"),
			},
			expectedVal: "secret-value",
			expectError: false,
			description: "should retrieve secret using default key",
		},
		{
			name: "successful retrieval with custom key",
			oidcConfig: &serviceApi.OIDCConfig{
				ClientSecretRef: corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "oidc-secret",
					},
					Key: "custom-key",
				},
			},
			secretData: map[string][]byte{
				"custom-key": []byte("custom-secret"),
			},
			expectedVal: "custom-secret",
			expectError: false,
			description: "should retrieve secret using custom key",
		},
		{
			name: "key not found in secret",
			oidcConfig: &serviceApi.OIDCConfig{
				ClientSecretRef: corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "oidc-secret",
					},
					Key: "missing-key",
				},
			},
			secretData: map[string][]byte{
				"other-key": []byte("other-value"),
			},
			expectedVal: "",
			expectError: true,
			description: "should return error when key not found",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)
			ctx := t.Context()

			// Create secret with test data
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tc.oidcConfig.ClientSecretRef.Name,
					Namespace: GatewayNamespace,
				},
				Data: tc.secretData,
			}

			client := setupTestClientWithObjects(secret)
			result, err := getOIDCClientSecret(ctx, client, tc.oidcConfig)

			if tc.expectError {
				g.Expect(err).To(HaveOccurred(), tc.description)
				g.Expect(result).To(Equal(""))
			} else {
				g.Expect(err).NotTo(HaveOccurred(), tc.description)
				g.Expect(result).To(Equal(tc.expectedVal), tc.description)
			}
		})
	}
}

// TestGetOIDCClientSecretNotFound tests error handling when secret doesn't exist.
func TestGetOIDCClientSecretNotFound(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := t.Context()

	oidcConfig := &serviceApi.OIDCConfig{
		ClientSecretRef: corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: "missing-secret",
			},
		},
	}

	client := setupTestClient() // No secrets
	result, err := getOIDCClientSecret(ctx, client, oidcConfig)

	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("failed to get OIDC client secret"))
	g.Expect(result).To(Equal(""))
}

// TestSecretHashAnnotationChangeTriggers tests that different secret values produce different annotations.
func TestSecretHashAnnotationChangeTriggers(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := t.Context()

	secretData1 := map[string]string{
		EnvClientID:     "client1",
		EnvClientSecret: "secret1",
		EnvCookieSecret: "cookie1",
	}
	secretData2 := map[string]string{
		EnvClientID:     "client2",
		EnvClientSecret: "secret2",
		EnvCookieSecret: "cookie2",
	}

	// Create first secret and deployment
	secretDataBytes1 := make(map[string][]byte)
	for k, v := range secretData1 {
		secretDataBytes1[k] = []byte(v)
	}
	secret1 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      KubeAuthProxySecretsName,
			Namespace: GatewayNamespace,
		},
		Data: secretDataBytes1,
	}
	client1 := setupTestClientWithObjects(secret1)
	rr1 := &odhtypes.ReconciliationRequest{Client: client1}

	// Create second secret and deployment
	secretDataBytes2 := make(map[string][]byte)
	for k, v := range secretData2 {
		secretDataBytes2[k] = []byte(v)
	}
	secret2 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      KubeAuthProxySecretsName,
			Namespace: GatewayNamespace,
		},
		Data: secretDataBytes2,
	}
	client2 := setupTestClientWithObjects(secret2)
	rr2 := &odhtypes.ReconciliationRequest{Client: client2}

	// Create two deployments with different secrets
	err1 := createKubeAuthProxyDeployment(ctx, rr1, nil, nil, expectedODHDomain)
	g.Expect(err1).NotTo(HaveOccurred())

	err2 := createKubeAuthProxyDeployment(ctx, rr2, nil, nil, expectedODHDomain)
	g.Expect(err2).NotTo(HaveOccurred())

	// Convert both to typed Deployments
	deployment1 := &appsv1.Deployment{}
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(rr1.Resources[0].Object, deployment1)
	g.Expect(err).NotTo(HaveOccurred())

	deployment2 := &appsv1.Deployment{}
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(rr2.Resources[0].Object, deployment2)
	g.Expect(err).NotTo(HaveOccurred())

	// Verify that the annotations are different
	annotation1 := deployment1.Spec.Template.Annotations["opendatahub.io/secret-hash"]
	annotation2 := deployment2.Spec.Template.Annotations["opendatahub.io/secret-hash"]

	g.Expect(annotation1).ToNot(BeEmpty(), "first deployment should have hash annotation")
	g.Expect(annotation2).ToNot(BeEmpty(), "second deployment should have hash annotation")
	g.Expect(annotation1).NotTo(Equal(annotation2), "different secrets should produce different hash annotations")
}

// TestSecretHashAnnotationWithoutSecret tests that deployment is created with empty hash when secret doesn't exist.
func TestSecretHashAnnotationWithoutSecret(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := t.Context()

	// Create client without any secrets
	client := setupTestClient()
	rr := &odhtypes.ReconciliationRequest{Client: client}

	// Create deployment without secret - should succeed with empty hash
	err := createKubeAuthProxyDeployment(ctx, rr, nil, nil, expectedODHDomain)
	g.Expect(err).NotTo(HaveOccurred(), "deployment creation should succeed even when secret doesn't exist")
	g.Expect(rr.Resources).To(HaveLen(1))

	// Convert to typed Deployment
	deployment := &appsv1.Deployment{}
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(rr.Resources[0].Object, deployment)
	g.Expect(err).NotTo(HaveOccurred())

	// Verify that the hash annotation exists but is empty
	g.Expect(deployment.Spec.Template.Annotations).To(HaveKey("opendatahub.io/secret-hash"),
		"pod template should have secret hash annotation even without secret")
	hash := deployment.Spec.Template.Annotations["opendatahub.io/secret-hash"]
	g.Expect(hash).To(BeEmpty(), "secret hash should be empty string when secret doesn't exist")
}
