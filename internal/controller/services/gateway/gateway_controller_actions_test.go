//nolint:testpackage
package gateway

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
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
	expectedHTTPSPort     = gwapiv1.PortNumber(443)
	expectedHTTPSProtocol = gwapiv1.HTTPSProtocolType
	expectedListenerName  = "https"

	// Auth-specific test constants.
	testOIDCIssuerURL = "https://auth.example.com"
	testAuthDomain    = "apps.example.com"
)

// setupTestClient creates a fake client with the required scheme for Gateway API tests.
func setupTestClient() client.Client {
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(gwapiv1.Install(scheme))
	utilruntime.Must(serviceApi.AddToScheme(scheme))
	utilruntime.Must(dsciv1.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).Build()
}

// setupTestClientWithObjects creates a fake client with pre-existing objects.
func setupTestClientWithObjects(objects ...client.Object) client.Client {
	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(gwapiv1.Install(scheme))
	utilruntime.Must(serviceApi.AddToScheme(scheme))
	utilruntime.Must(dsciv1.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objects...).Build()
}

// gatewayConfigWithCert creates a test GatewayConfig with certificate spec.
func gatewayConfigWithCert(name string, certType infrav1.CertType, secretName ...string) *serviceApi.GatewayConfig {
	gc := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: serviceApi.GatewayConfigSpec{
			Certificate: &infrav1.CertificateSpec{Type: certType},
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
			Domain: domain,
			Certificate: &infrav1.CertificateSpec{
				Type: certType,
			},
		},
	}
}

// convertToGatewayClass converts unstructured resource to typed GatewayClass.
func convertToGatewayClass(g *WithT, resource *unstructured.Unstructured) *gwapiv1.GatewayClass {
	gatewayClass := &gwapiv1.GatewayClass{}
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, gatewayClass)
	g.Expect(err).NotTo(HaveOccurred())
	return gatewayClass
}

// convertToGateway converts unstructured resource to typed Gateway.
func convertToGateway(g *WithT, resource *unstructured.Unstructured) *gwapiv1.Gateway {
	gateway := &gwapiv1.Gateway{}
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(resource.Object, gateway)
	g.Expect(err).NotTo(HaveOccurred())
	return gateway
}

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
			err := createGateway(rr, test.cert, test.domain, test.name)

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

	rr := &odhtypes.ReconciliationRequest{
		Client:   setupTestClient(),
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
			name:        "OIDC mode with valid config",
			authMode:    AuthModeOIDC,
			oidcConfig:  &serviceApi.OIDCConfig{IssuerURL: testOIDCIssuerURL},
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

	domain := "data-science-gateway.apps.example.com"

	args := buildOAuth2ProxyArgs(nil, nil, domain) // nil OIDC = OpenShift mode, nil cookie = defaults

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

	domain := "data-science-gateway.apps.example.com"

	oidcConfig := &serviceApi.OIDCConfig{
		IssuerURL: testOIDCIssuerURL,
	}

	args := buildOAuth2ProxyArgs(oidcConfig, nil, domain) // nil cookie = defaults

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
