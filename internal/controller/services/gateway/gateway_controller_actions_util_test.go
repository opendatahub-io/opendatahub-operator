//nolint:testpackage
package gateway

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

const (
	// Test constants.
	testCertSecret = "test-cert-secret"
	testDomain     = "gateway.example.com"
	odhTestCert    = "odh-cert"
	odhTestDomain  = "data-science-gateway.apps.cluster.example.com"
)

// setupTestClient creates a fake client with the required scheme for Gateway API tests.
func setupTestClient() client.Client {
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(gwapiv1.Install(scheme))
	utilruntime.Must(serviceApi.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).Build()
}

// setupTestClientWithObjects creates a fake client with pre-existing objects.
func setupTestClientWithObjects(objects ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(gwapiv1.Install(scheme))
	utilruntime.Must(serviceApi.AddToScheme(scheme))
	utilruntime.Must(dsciv2.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

// gatewayConfigWithCert creates a test GatewayConfig with certificate spec.
func gatewayConfigWithCert(name string, certType infrav1.CertType, secretName ...string) *serviceApi.GatewayConfig {
	gc := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: serviceApi.GatewayConfigSpec{
			Cookie: serviceApi.CookieConfig{
				Expire:  metav1.Duration{Duration: 24 * time.Hour},
				Refresh: metav1.Duration{Duration: 2 * time.Hour},
			},
			Certificate: &infrav1.CertificateSpec{
				Type: certType,
			},
		},
	}
	if len(secretName) > 0 && secretName[0] != "" {
		gc.Spec.Certificate.SecretName = secretName[0]
	}
	return gc
}

// gatewayConfigNoCert creates a test GatewayConfig without certificate spec.
func gatewayConfigNoCert(name string) *serviceApi.GatewayConfig {
	return &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       serviceApi.GatewayConfigSpec{},
	}
}

// createTestGatewayConfig creates a test GatewayConfig for testing.
func createTestGatewayConfig(name, domain string, certType infrav1.CertType) *serviceApi.GatewayConfig {
	return &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: serviceApi.GatewayConfigSpec{
			Cookie: serviceApi.CookieConfig{
				Expire:  metav1.Duration{Duration: 24 * time.Hour},
				Refresh: metav1.Duration{Duration: 2 * time.Hour},
			},
			Domain: domain,
			Certificate: &infrav1.CertificateSpec{
				Type: certType,
			},
		},
	}
}

// createMockOpenShiftIngress creates a mock OpenShift Ingress object for testing cluster domain.
func createMockOpenShiftIngress(domain string) client.Object {
	ingress := &unstructured.Unstructured{}
	ingress.SetGroupVersionKind(gvk.OpenshiftIngress)
	ingress.SetName("cluster")

	// Set the spec.domain field that cluster.GetDomain expects
	if err := unstructured.SetNestedField(ingress.Object, domain, "spec", "domain"); err != nil {
		panic(err) // This should never happen in tests
	}

	return ingress
}

func TestGetFQDN(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		gatewayConfig *serviceApi.GatewayConfig
		expectedFQDN  string
		expectError   bool
	}{
		{
			name: "Use cluster default Ingress CR only",
			gatewayConfig: &serviceApi.GatewayConfig{
				Spec: serviceApi.GatewayConfigSpec{
					Cookie:      serviceApi.CookieConfig{Expire: metav1.Duration{Duration: 24 * time.Hour}, Refresh: metav1.Duration{Duration: 2 * time.Hour}},
					Certificate: &infrav1.CertificateSpec{},
				},
			},
			expectedFQDN: "data-science-gateway.apps.cluster.example.com",
			expectError:  false,
		},
		{
			name: "Custom subdomain with default Ingress CR domain",
			gatewayConfig: &serviceApi.GatewayConfig{
				Spec: serviceApi.GatewayConfigSpec{
					Cookie:      serviceApi.CookieConfig{Expire: metav1.Duration{Duration: 24 * time.Hour}, Refresh: metav1.Duration{Duration: 2 * time.Hour}},
					Subdomain:   "one-custom-gateway",
					Certificate: &infrav1.CertificateSpec{},
				},
			},
			expectedFQDN: "one-custom-gateway.apps.cluster.example.com",
			expectError:  false,
		},
		{
			name: "Default subdomain with custom domain",
			gatewayConfig: &serviceApi.GatewayConfig{
				Spec: serviceApi.GatewayConfigSpec{
					Cookie:      serviceApi.CookieConfig{Expire: metav1.Duration{Duration: 24 * time.Hour}, Refresh: metav1.Duration{Duration: 2 * time.Hour}},
					Domain:      "one.domain.com",
					Certificate: &infrav1.CertificateSpec{},
				},
			},
			expectedFQDN: "data-science-gateway.one.domain.com",
			expectError:  false,
		},
		{
			name: "Whitespace trimming for both subdomain and domain",
			gatewayConfig: &serviceApi.GatewayConfig{
				Spec: serviceApi.GatewayConfigSpec{
					Cookie:      serviceApi.CookieConfig{Expire: metav1.Duration{Duration: 24 * time.Hour}, Refresh: metav1.Duration{Duration: 2 * time.Hour}},
					Domain:      "  one.domain.com ",
					Subdomain:   " one-gateway  ",
					Certificate: &infrav1.CertificateSpec{},
				},
			},
			expectedFQDN: "one-gateway.one.domain.com",
			expectError:  false,
		},
		{
			name: "Empty subdomain uses default value",
			gatewayConfig: &serviceApi.GatewayConfig{
				Spec: serviceApi.GatewayConfigSpec{
					Cookie:      serviceApi.CookieConfig{Expire: metav1.Duration{Duration: 24 * time.Hour}, Refresh: metav1.Duration{Duration: 2 * time.Hour}},
					Domain:      "one.domain.com",
					Subdomain:   "",
					Certificate: &infrav1.CertificateSpec{},
				},
			},
			expectedFQDN: "data-science-gateway.one.domain.com",
			expectError:  false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			ingress := createMockOpenShiftIngress("apps.cluster.example.com")
			cli := setupTestClientWithObjects(ingress)

			fqdn, err := GetFQDN(ctx, cli, tc.gatewayConfig)
			if tc.expectError {
				if err == nil {
					t.Errorf("Expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			// Verify FQDN
			if fqdn != tc.expectedFQDN {
				t.Errorf("GetFQDN() = %q, want %q", fqdn, tc.expectedFQDN)
			}
		})
	}
}

func TestGetFQDN_ClusterDomainError(t *testing.T) {
	ctx := context.Background()

	// Create client without OpenShift Ingress - should fail when trying to get cluster domain
	cli := setupTestClient()

	gatewayConfig := &serviceApi.GatewayConfig{
		Spec: serviceApi.GatewayConfigSpec{
			Cookie: serviceApi.CookieConfig{Expire: metav1.Duration{Duration: 24 * time.Hour}, Refresh: metav1.Duration{Duration: 2 * time.Hour}},
			// No Domain - will try to fetch cluster domain and fail
			Certificate: &infrav1.CertificateSpec{},
		},
	}

	_, err := GetFQDN(ctx, cli, gatewayConfig)
	if err == nil {
		t.Error("Expected error when cluster domain cannot be fetched, but got nil")
	}
}
