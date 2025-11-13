//nolint:testpackage
package gateway

import (
	"fmt"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

	. "github.com/onsi/gomega"
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

	// Verify the GatewayClass resource has correct properties directly from unstructured
	resource := &rr.Resources[0]
	g.Expect(resource.GetName()).To(Equal(GatewayClassName))
	g.Expect(resource.GetKind()).To(Equal("GatewayClass"))

	controllerName, found, err := unstructured.NestedString(resource.Object, "spec", "controllerName")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(controllerName).To(Equal(GatewayControllerName))
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
		{testCertSecret, testDomain, "with_cert", 1},
		{"", testDomain, "no_cert", 0},
		{odhTestCert, odhTestDomain, "odh_cert", 1},
	}

	for _, test := range tests {
		name := fmt.Sprintf("%s_%d_listeners", test.name, test.listeners)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := NewWithT(t)

			rr := &odhtypes.ReconciliationRequest{Client: setupTestClient()}
			err := createGateway(rr, test.cert, test.domain)

			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(rr.Resources).To(HaveLen(1))

			resource := &rr.Resources[0]
			g.Expect(resource.GetName()).To(Equal(DefaultGatewayName))
			g.Expect(resource.GetNamespace()).To(Equal(GatewayNamespace))

			listeners, found, err := unstructured.NestedSlice(resource.Object, "spec", "listeners")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(found).To(BeTrue())
			g.Expect(listeners).To(HaveLen(test.listeners))

			if test.listeners > 0 {
				listener, ok := listeners[0].(map[string]interface{})
				g.Expect(ok).To(BeTrue(), "listener should be a map[string]interface{}")
				hostname, found, err := unstructured.NestedString(listener, "hostname")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(found).To(BeTrue())
				g.Expect(hostname).To(Equal(test.domain))

				name, found, err := unstructured.NestedString(listener, "name")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(found).To(BeTrue())
				g.Expect(name).To(Equal("https"))

				if test.cert != "" {
					certRefs, found, err := unstructured.NestedSlice(listener, "tls", "certificateRefs")
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(found).To(BeTrue())
					g.Expect(certRefs).To(HaveLen(1))

					certRef, ok := certRefs[0].(map[string]interface{})
					g.Expect(ok).To(BeTrue(), "certRef should be a map[string]interface{}")
					certName, found, err := unstructured.NestedString(certRef, "name")
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(found).To(BeTrue())
					g.Expect(certName).To(Equal(test.cert))
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

func TestGetCertificateType(t *testing.T) {
	g := NewWithT(t)

	t.Run("returns default when certificate type not specified by user", func(t *testing.T) {
		gateway := &serviceApi.GatewayConfig{
			Spec: serviceApi.GatewayConfigSpec{
				Cookie: serviceApi.CookieConfig{
					Expire:  metav1.Duration{Duration: 24 * time.Hour},
					Refresh: metav1.Duration{Duration: 2 * time.Hour},
				},
				Certificate: &infrav1.CertificateSpec{
					SecretName: "some-secret",
				},
			},
		}

		certType := getCertificateType(gateway)
		g.Expect(certType).To(Equal(string(infrav1.OpenshiftDefaultIngress)))
	})

	t.Run("returns certificate type when specified", func(t *testing.T) {
		gateway := &serviceApi.GatewayConfig{
			Spec: serviceApi.GatewayConfigSpec{
				Cookie: serviceApi.CookieConfig{Expire: metav1.Duration{Duration: 24 * time.Hour}, Refresh: metav1.Duration{Duration: 2 * time.Hour}},
				Certificate: &infrav1.CertificateSpec{
					Type: infrav1.SelfSigned,
				},
			},
		}

		certType := getCertificateType(gateway)
		g.Expect(certType).To(Equal(string(infrav1.SelfSigned)))
	})
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
				Client:     client,
				Instance:   tc.gatewayConfig,
				Conditions: conditions.NewManager(tc.gatewayConfig, ReadyConditionType),
			}

			err := syncGatewayConfigStatus(ctx, rr)
			g.Expect(err).NotTo(HaveOccurred(), tc.description)

			// Verify the condition was set correctly
			conditions := tc.gatewayConfig.GetConditions()
			g.Expect(conditions).To(HaveLen(1))
			condition := conditions[0]
			g.Expect(condition.Type).To(Equal(ReadyConditionType))
			g.Expect(condition.Status).To(Equal(tc.expectedCondition))
			g.Expect(condition.Reason).To(Equal(tc.expectedReason))
			g.Expect(condition.Message).To(Equal(tc.expectedMessage))
		})
	}
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
	gatewayClassResource := &rr.Resources[0]
	g.Expect(gatewayClassResource.GetName()).To(Equal(GatewayClassName))

	// Verify Gateway
	gatewayResource := &rr.Resources[1]
	expectedGatewayName := DefaultGatewayName
	g.Expect(gatewayResource.GetName()).To(Equal(expectedGatewayName))
	g.Expect(gatewayResource.GetNamespace()).To(Equal(GatewayNamespace))
}

// TestAuthProxyTimeout tests the auth proxy timeout logic.
func TestAuthProxyTimeout(t *testing.T) {
	testCases := []struct {
		name            string
		timeoutValue    string
		envVarValue     string
		expectedTimeout string
	}{
		{
			name:            "Timeout explicitly set to 2s in GatewayConfig",
			timeoutValue:    "2s",
			envVarValue:     "",
			expectedTimeout: "2s",
		},
		{
			name:            "Timeout set, env variable GATEWAY_AUTH_TIMEOUT also set",
			timeoutValue:    "1s",
			envVarValue:     "6s",
			expectedTimeout: "1s",
		},
		{
			name:            "Timeout not set and env variable GATEWAY_AUTH_TIMEOUT not set, uses default 5s",
			timeoutValue:    "0s", // zero value when not set
			envVarValue:     "",
			expectedTimeout: "5s",
		},
		{
			name:            "Timeout not set, env variable GATEWAY_AUTH_TIMEOUT set to 4s",
			timeoutValue:    "0s",
			envVarValue:     "4s",
			expectedTimeout: "4s",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			ctx := t.Context()

			if tc.envVarValue != "" {
				t.Setenv("GATEWAY_AUTH_TIMEOUT", tc.envVarValue)
			}

			gatewayConfig := createTestGatewayConfig("test-gateway", testDomain, infrav1.Provided)
			if tc.timeoutValue != "0s" {
				duration, err := time.ParseDuration(tc.timeoutValue)
				g.Expect(err).NotTo(HaveOccurred())
				gatewayConfig.Spec.AuthProxyTimeout = metav1.Duration{Duration: duration}
			}
			// else: zero value (not set)

			ingress := createMockOpenShiftIngress("apps.cluster.example.com")
			cli := setupTestClientWithObjects(ingress)

			rr := &odhtypes.ReconciliationRequest{
				Client:   cli,
				Instance: gatewayConfig,
			}

			templateData, err := getTemplateData(ctx, rr)
			g.Expect(err).NotTo(HaveOccurred())

			// Verify the timeout value in template data
			actualTimeout, exists := templateData["AuthProxyTimeout"]
			g.Expect(exists).To(BeTrue())
			g.Expect(actualTimeout).To(Equal(tc.expectedTimeout))
		})
	}
}
