//nolint:testpackage
package gateway

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"

	. "github.com/onsi/gomega"
)

const (
	// Test constants for gateway support functions.
	testCertSecretSupport = "test-cert-secret"
	testDomainSupport     = "data-science-gateway.apps.test-cluster.com"
	testClusterDomain     = "apps.cluster.example.com"
	testUserDomain        = "apps.example.com"
	customCertSecret      = "my-cert"
	unknownPlatform       = "unknown-platform"

	// Expected values constants.
	expectedHTTPSListenerName    = "https"
	expectedHTTPSPortSupport     = gwapiv1.PortNumber(443)
	expectedHTTPSProtocolSupport = gwapiv1.HTTPSProtocolType
	expectedODHDomain            = "data-science-gateway.apps.example.com"
	expectedClusterDomain        = "data-science-gateway.apps.cluster.example.com"
)

// createTestScheme creates a reusable scheme for test clients.
func createTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	utilruntime.Must(serviceApi.AddToScheme(scheme))
	return scheme
}

// setupSupportTestClient creates a fake client with required schemes for support function tests.
func setupSupportTestClient() client.Client {
	return fake.NewClientBuilder().WithScheme(createTestScheme()).Build()
}

// setupSupportTestClientWithClusterIngress creates a fake client with a mock cluster ingress object.
func setupSupportTestClientWithClusterIngress(domain string) client.Client {
	clusterIngress := &unstructured.Unstructured{}
	clusterIngress.SetGroupVersionKind(gvk.OpenshiftIngress)
	clusterIngress.SetName("cluster")

	// Set the spec.domain field
	_ = unstructured.SetNestedField(clusterIngress.Object, domain, "spec", "domain")

	return fake.NewClientBuilder().WithScheme(createTestScheme()).WithObjects(clusterIngress).Build()
}

// createTestGatewayConfigSupport creates a GatewayConfig for support function testing.
func createTestGatewayConfigSupport(domain string, certSpec *infrav1.CertificateSpec) *serviceApi.GatewayConfig {
	return &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.GatewayInstanceName,
		},
		Spec: serviceApi.GatewayConfigSpec{
			Domain:      domain,
			Certificate: certSpec,
		},
	}
}

// TestCreateListeners tests the CreateListeners helper function.
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
			listeners := CreateListeners(tc.certSecret, tc.domain)

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

// TestGetCertificateType tests the GetCertificateType helper function.
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

			certType := GetCertificateType(gateway)
			g.Expect(certType).To(Equal(tc.expectedType), tc.description)
		})
	}
}

// TestResolveDomain tests the ResolveDomain helper function.
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

			domain, err := ResolveDomain(ctx, client, gatewayConfig)

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

// TestCreateListenersEdgeCases tests edge cases for the CreateListeners function.
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
			listeners := CreateListeners(tc.certSecret, tc.domain)

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

// TestResolveDomainNilHandling tests nil parameter handling for ResolveDomain.
func TestResolveDomainNilHandling(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	ctx := t.Context()
	client := setupSupportTestClient()

	// Test nil gatewayConfig - should fall back to cluster domain (which will fail with our test client)
	domain, err := ResolveDomain(ctx, client, nil)
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

			domain, err := ResolveDomain(ctx, client, gatewayConfig)

			g.Expect(err).ToNot(HaveOccurred(), tc.description)
			g.Expect(domain).To(ContainSubstring(tc.expectedContains), tc.description)
		})
	}
}
