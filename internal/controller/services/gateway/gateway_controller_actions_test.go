//nolint:testpackage
package gateway

import (
	"fmt"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

	. "github.com/onsi/gomega"
)

const (
	// Test constants.
	testCertSecret        = "test-cert-secret"
	testDomain            = "gateway.example.com"
	testGatewayName       = "test-gateway"
	testGatewayNameNoCert = "test-gateway-no-cert"
	odhTestCert           = "odh-cert"
	odhTestDomain         = "data-science-gateway.apps.cluster.example.com"

	// Auth-specific test constants.
	testOIDCIssuerURL = "https://auth.example.com"
)

// TestCreateGatewayClass tests the createGatewayClass controller action function.
func TestCreateGatewayClass(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	rr := &odhtypes.ReconciliationRequest{
		Client: setupTestClient(),
	}

	err := createGatewayClass(rr)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(rr.Resources).To(HaveLen(1))

	// Convert unstructured to typed GatewayClass
	gatewayClass := convertToGatewayClass(g, &rr.Resources[0])

	// Verify the GatewayClass resource has correct properties using typed access
	g.Expect(gatewayClass.GetName()).To(Equal(GatewayClassName))
	g.Expect(gatewayClass.Kind).To(Equal("GatewayClass"))
	g.Expect(string(gatewayClass.Spec.ControllerName)).To(Equal(GatewayControllerName))
}

// TestCreateGateway tests the createGateway controller action function with different scenarios.
func TestCreateGateway(t *testing.T) {
	t.Parallel()

	tests := []struct {
		cert      string
		domain    string
		name      string
		listeners int
	}{
		{testCertSecret, testDomain, testGatewayName, 1},
		{"", testDomain, testGatewayNameNoCert, 0},
		{odhTestCert, odhTestDomain, DefaultGatewayName, 1},
	}

	for _, test := range tests {
		name := fmt.Sprintf("%s_%d_listeners", test.name, test.listeners)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			rr := &odhtypes.ReconciliationRequest{Client: setupTestClient()}
			err := createGateway(rr, test.cert, test.domain, "opendatahub", test.name)

			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(rr.Resources).To(HaveLen(1))

			gateway := convertToGateway(g, &rr.Resources[0])
			g.Expect(gateway.GetName()).To(Equal(test.name))
			g.Expect(gateway.GetNamespace()).To(Equal(GatewayNamespace))
			g.Expect(gateway.Spec.Listeners).To(HaveLen(test.listeners))

			if test.listeners > 0 {
				listener := gateway.Spec.Listeners[0]
				g.Expect(string(*listener.Hostname)).To(Equal(test.domain))
				g.Expect(string(listener.Name)).To(Equal("https"))
				if test.cert != "" {
					g.Expect(string(listener.TLS.CertificateRefs[0].Name)).To(Equal(test.cert))
				}
			}
		})
	}
}

// TestHandleCertificates tests the handleCertificates function with different certificate types.
func TestHandleCertificates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config *serviceApi.GatewayConfig
		secret string
		hasErr bool
	}{
		{"provided_with_secret", gatewayConfigWithCert("gw", infrav1.Provided, "my-secret"), "my-secret", false},
		{"provided_no_secret", gatewayConfigWithCert("gw", infrav1.Provided), "gw-tls", false},
		{"nil_certificate", gatewayConfigNoCert("gw"), "gw-tls", true},
		{"unsupported_type", gatewayConfigWithCert("gw", "BadType"), "", true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			rr := &odhtypes.ReconciliationRequest{Client: setupTestClient()}
			secret, err := handleCertificates(t.Context(), rr, test.config, testDomain)

			if test.hasErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(secret).To(Equal(test.secret))
			}
		})
	}
}

// TestSyncGatewayConfigStatus tests the syncGatewayConfigStatus function.
func TestSyncGatewayConfigStatus(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name              string
		gatewayConfig     *serviceApi.GatewayConfig
		gateway           *gwapiv1.Gateway
		expectedCondition metav1.ConditionStatus
		expectedReason    string
		expectedMessage   string
		gatewayNotFound   bool
		description       string
	}{
		{
			name: "gateway ready condition true",
			gatewayConfig: &serviceApi.GatewayConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-gateway"},
			},
			gateway: &gwapiv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DefaultGatewayName,
					Namespace: GatewayNamespace,
				},
				Status: gwapiv1.GatewayStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(gwapiv1.GatewayConditionAccepted),
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			expectedCondition: metav1.ConditionTrue,
			expectedReason:    status.ReadyReason,
			expectedMessage:   status.GatewayReadyMessage,
			description:       "should set ready condition when gateway is accepted",
		},
		{
			name: "gateway not ready condition false",
			gatewayConfig: &serviceApi.GatewayConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-gateway"},
			},
			gateway: &gwapiv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      DefaultGatewayName,
					Namespace: GatewayNamespace,
				},
				Status: gwapiv1.GatewayStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(gwapiv1.GatewayConditionAccepted),
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
			expectedCondition: metav1.ConditionFalse,
			expectedReason:    status.NotReadyReason,
			expectedMessage:   status.GatewayNotReadyMessage,
			description:       "should set not ready condition when gateway is not accepted",
		},
		{
			name: "gateway not found",
			gatewayConfig: &serviceApi.GatewayConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "test-gateway"},
			},
			gatewayNotFound:   true,
			expectedCondition: metav1.ConditionFalse,
			expectedReason:    status.NotReadyReason,
			expectedMessage:   status.GatewayNotFoundMessage,
			description:       "should set not found condition when gateway doesn't exist",
		},
	}

	for _, tc := range testCases {
		// capture range var for parallel subtests
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)
			ctx := t.Context()

			var client client.Client
			if tc.gatewayNotFound {
				client = setupTestClient()
			} else {
				client = setupTestClientWithObjects(tc.gateway)
			}

			rr := &odhtypes.ReconciliationRequest{
				Client:   client,
				Instance: tc.gatewayConfig,
			}

			err := syncGatewayConfigStatus(ctx, rr)
			g.Expect(err).NotTo(HaveOccurred(), tc.description)

			// Verify the condition was set correctly
			conditions := tc.gatewayConfig.GetConditions()
			g.Expect(conditions).To(HaveLen(1))
			condition := conditions[0]
			g.Expect(condition.Type).To(Equal(status.ConditionTypeReady))
			g.Expect(condition.Status).To(Equal(tc.expectedCondition))
			g.Expect(condition.Reason).To(Equal(tc.expectedReason))
			g.Expect(condition.Message).To(Equal(tc.expectedMessage))
		})
	}
}

// TestSyncGatewayConfigStatusInvalidInstance tests error handling for invalid instance type.
func TestSyncGatewayConfigStatusInvalidInstance(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	ctx := t.Context()
	rr := &odhtypes.ReconciliationRequest{
		Client:   setupTestClient(),
		Instance: &serviceApi.Auth{}, // Wrong type
	}

	err := syncGatewayConfigStatus(ctx, rr)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("instance is not of type *services.GatewayConfig"))
}

// TestCreateGatewayInfrastructureInvalidInstance tests error handling for invalid instance type.
func TestCreateGatewayInfrastructureInvalidInstance(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	ctx := t.Context()
	rr := &odhtypes.ReconciliationRequest{
		Client:   setupTestClient(),
		Instance: &serviceApi.Auth{}, // Wrong type
	}

	err := createGatewayInfrastructure(ctx, rr)
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("instance is not of type *services.GatewayConfig"))
}

// TestCreateGatewayInfrastructureWithProvidedCertificate tests the main orchestrator function with provided certificate.
func TestCreateGatewayInfrastructureWithProvidedCertificate(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)

	ctx := t.Context()
	gatewayConfig := createTestGatewayConfig("test-gateway", testDomain, infrav1.Provided)
	gatewayConfig.Spec.Certificate.SecretName = testCertSecret

	// Create mock DSCI object
	dsci := &dsciv2.DSCInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-dsci",
		},
		Spec: dsciv2.DSCInitializationSpec{
			ApplicationsNamespace: "opendatahub",
		},
	}

	rr := &odhtypes.ReconciliationRequest{
		Client:   setupTestClientWithObjects(dsci),
		Instance: gatewayConfig,
	}

	err := createGatewayInfrastructure(ctx, rr)
	g.Expect(err).NotTo(HaveOccurred())

	// Should create GatewayClass and Gateway resources
	g.Expect(rr.Resources).To(HaveLen(2))

	// Verify GatewayClass
	gatewayClass := convertToGatewayClass(g, &rr.Resources[0])
	g.Expect(gatewayClass.GetName()).To(Equal(GatewayClassName))

	// Verify Gateway
	gateway := convertToGateway(g, &rr.Resources[1])
	expectedGatewayName := DefaultGatewayName
	g.Expect(gateway.GetName()).To(Equal(expectedGatewayName))
	g.Expect(gateway.GetNamespace()).To(Equal(GatewayNamespace))
}
