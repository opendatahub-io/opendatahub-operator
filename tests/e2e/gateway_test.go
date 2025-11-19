package e2e_test

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"sync"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

// Gateway TLS and EnvoyFilter configuration constants.
const (
	gatewayTLSSecretName   = "default-gateway-tls"
	envoyFilterName        = "authn-filter"
	expectedSecretDataKeys = 3
)

// Gateway infrastructure and OAuth proxy configuration constants.
// These match the values defined in internal/controller/services/gateway package.
const (
	gatewayConfigName        = serviceApi.GatewayInstanceName
	gatewayName              = gateway.DefaultGatewayName
	gatewayClassName         = gateway.GatewayClassName
	gatewayNamespace         = gateway.GatewayNamespace
	oauthClientName          = gateway.AuthClientID
	kubeAuthProxyName        = gateway.KubeAuthProxyName
	kubeAuthProxyTLSName     = gateway.KubeAuthProxyTLSName
	kubeAuthProxyCredsName   = gateway.KubeAuthProxySecretsName
	oauthCallbackRouteName   = gateway.OAuthCallbackRouteName
	authProxyOAuth2Path      = gateway.AuthProxyOAuth2Path
	kubeAuthProxyHTTPPort    = gateway.AuthProxyHTTPPort
	kubeAuthProxyHTTPSPort   = gateway.AuthProxyHTTPSPort
	kubeAuthProxyMetricsPort = gateway.AuthProxyMetricsPort
)

type GatewayTestCtx struct {
	*TestContext

	// cachedGatewayHostname stores the computed gateway hostname to avoid repeated cluster API calls.
	cachedGatewayHostname string
	// once ensures thread-safe lazy initialization of cachedGatewayHostname.
	once sync.Once
}

func gatewayTestSuite(t *testing.T) {
	t.Helper()

	ctx, err := NewTestContext(t)
	require.NoError(t, err)

	gatewayCtx := &GatewayTestCtx{
		TestContext: ctx,
	}

	testCases := []TestCase{
		{"Validate GatewayConfig creation", gatewayCtx.ValidateGatewayConfig},
		{"Validate Gateway infrastructure", gatewayCtx.ValidateGatewayInfrastructure},
		{"Validate OAuth client and secret creation", gatewayCtx.ValidateOAuthClientAndSecret},
		{"Validate authentication proxy deployment", gatewayCtx.ValidateAuthProxyDeployment},
		{"Validate OAuth callback HTTPRoute", gatewayCtx.ValidateOAuthCallbackRoute},
		{"Validate EnvoyFilter creation", gatewayCtx.ValidateEnvoyFilter},
		{"Validate Gateway ready status", gatewayCtx.ValidateGatewayReadyStatus},
		{"Validate unauthenticated access redirects to login", gatewayCtx.ValidateUnauthenticatedRedirect},
	}

	RunTestCases(t, testCases)
}

// makeRedirectURL constructs the OAuth redirect URL for the authentication proxy.
// Format: https://<gateway-hostname>/oauth2/callback
func makeRedirectURL(hostname string) string {
	return fmt.Sprintf("--redirect-url=https://%s%s/callback", hostname, authProxyOAuth2Path)
}

// makeCookieDomain constructs the cookie domain argument for the authentication proxy.
// Ensures OAuth cookies work across all routes on the gateway.
func makeCookieDomain(hostname string) string {
	return fmt.Sprintf("--cookie-domain=%s", hostname)
}

// ValidateGatewayConfig ensures the GatewayConfig CR exists and is properly configured.
func (tc *GatewayTestCtx) ValidateGatewayConfig(t *testing.T) {
	t.Helper()
	t.Log("Validating GatewayConfig resource")

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.GatewayConfig, types.NamespacedName{Name: gatewayConfigName}),
		WithCondition(And(
			jq.Match(`.spec.certificate.secretName == "%s"`, gatewayTLSSecretName),
			jq.Match(`.spec.certificate.type == "%s"`, string(infrav1.OpenshiftDefaultIngress)),
			jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "%s"`, metav1.ConditionTrue),
		)),
		WithCustomErrorMsg("GatewayConfig should have correct certificate configuration and Ready status"),
	)

	t.Log("GatewayConfig validation completed")
}

// ValidateGatewayInfrastructure validates Gateway API resources (GatewayClass, Gateway, TLS).
func (tc *GatewayTestCtx) ValidateGatewayInfrastructure(t *testing.T) {
	t.Helper()
	t.Log("Validating Gateway infrastructure resources")

	t.Log("Validating GatewayClass resource")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.GatewayClass, types.NamespacedName{Name: gatewayClassName}),
		WithCondition(jq.Match(`.spec.controllerName == "%s"`, gateway.GatewayControllerName)),
		WithCustomErrorMsg("GatewayClass should exist with OpenShift Gateway controller"),
	)

	t.Log("Validating TLS certificate secret")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Secret, types.NamespacedName{
			Name:      gatewayTLSSecretName,
			Namespace: gatewayNamespace,
		}),
		WithCustomErrorMsg("TLS secret should exist"),
	)

	expectedGatewayHostname := tc.getExpectedGatewayHostname(t)

	t.Log("Validating Gateway resource")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.KubernetesGateway, types.NamespacedName{
			Name:      gatewayName,
			Namespace: gatewayNamespace,
		}),
		WithCondition(And(
			jq.Match(`.spec.gatewayClassName == "%s"`, gatewayClassName),
			jq.Match(`.spec.listeners | length > 0`),
			jq.Match(`.spec.listeners[] | select(.name == "https") | .protocol == "%s"`, string(gwapiv1.HTTPSProtocolType)),
			jq.Match(`.spec.listeners[] | select(.name == "https") | .port == 443`),
			jq.Match(`.spec.listeners[] | select(.name == "https") | .hostname == "%s"`, expectedGatewayHostname),
			jq.Match(`.spec.listeners[] | select(.name == "https") | .tls.certificateRefs[0].name == "%s"`, gatewayTLSSecretName),
		)),
		WithCustomErrorMsg("Gateway should be created with correct HTTPS listener configuration and hostname %s", expectedGatewayHostname),
	)

	t.Log("Gateway infrastructure validation completed")
}

// ValidateOAuthClientAndSecret validates OpenShift OAuth client and proxy secret creation.
func (tc *GatewayTestCtx) ValidateOAuthClientAndSecret(t *testing.T) {
	t.Helper()
	t.Log("Validating OAuth client and secret creation")

	expectedGatewayHostname := tc.getExpectedGatewayHostname(t)
	expectedRedirectURI := "https://" + expectedGatewayHostname + authProxyOAuth2Path + "/callback"

	// OAuthClient
	t.Log("Validating OAuthClient resource")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.OAuthClient, types.NamespacedName{Name: oauthClientName}),
		WithCondition(And(
			jq.Match(`.grantMethod == "auto"`),
			jq.Match(`.redirectURIs | length > 0`),
			jq.Match(`.redirectURIs[] | . == "%s"`, expectedRedirectURI),
			jq.Match(`.secret | length > 0`),
		)),
		WithCustomErrorMsg("OAuthClient should exist with auto grant method, correct OAuth callback redirect URI (%s), and non-empty secret", expectedRedirectURI),
	)
	t.Log("OAuthClient validated successfully")

	// OAuth proxy credentials secret
	t.Log("Validating OAuth proxy credentials secret")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Secret, types.NamespacedName{
			Name:      kubeAuthProxyCredsName,
			Namespace: gatewayNamespace,
		}),
		WithCondition(And(
			jq.Match(`.type == "%s"`, string(corev1.SecretTypeOpaque)),
			jq.Match(`.metadata.labels.app == "%s"`, kubeAuthProxyName),
			jq.Match(`.data | has("OAUTH2_PROXY_CLIENT_ID")`),
			jq.Match(`.data | has("OAUTH2_PROXY_CLIENT_SECRET")`),
			jq.Match(`.data | has("OAUTH2_PROXY_COOKIE_SECRET")`),
			jq.Match(`.data.OAUTH2_PROXY_CLIENT_SECRET | length > 0`),
			jq.Match(`.data.OAUTH2_PROXY_COOKIE_SECRET | length > 0`),
		)),
		WithCustomErrorMsg("OAuth proxy credentials secret should be Opaque type with app label, "+
			"exactly %d non-empty keys, and CLIENT_ID matching OAuthClient name", expectedSecretDataKeys),
	)

	t.Log("OAuth client and secret validation completed")
}

// ValidateAuthProxyDeployment validates the kube-auth-proxy deployment and service.
//
// The kube-auth-proxy acts as an OAuth2 proxy that:
// 1. Intercepts unauthenticated requests via EnvoyFilter external authorization
// 2. Redirects users to OpenShift OAuth provider for authentication
// 3. Handles OAuth callback and sets authentication cookies
// 4. Validates authentication on subsequent requests
//
// This test verifies:
// - Deployment exists with correct configuration and secret hash annotation
// - Service exposes HTTP (8080), HTTPS (8443), and metrics (9091) ports
// - Container args include proper redirect URL and cookie domain
// - TLS certificates are properly mounted.
func (tc *GatewayTestCtx) ValidateAuthProxyDeployment(t *testing.T) {
	t.Helper()
	t.Log("Validating kube-auth-proxy deployment and service")

	expectedGatewayHostname := tc.getExpectedGatewayHostname(t)
	expectedRedirectURL := makeRedirectURL(expectedGatewayHostname)
	expectedCookieDomain := makeCookieDomain(expectedGatewayHostname)

	// kube-auth-proxy deployment checks (many conditions grouped into a single EnsureResourceExists call)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      kubeAuthProxyName,
			Namespace: gatewayNamespace,
		}),
		WithCondition(And(
			// basic pod template checks
			jq.Match(`.spec.selector.matchLabels.app == "%s"`, kubeAuthProxyName),
			jq.Match(`.spec.template.spec.containers | length > 0`),
			jq.Match(`.spec.template.spec.containers[0].name == "%s"`, kubeAuthProxyName),

			// ports
			jq.Match(`.spec.template.spec.containers[0].ports | length == 3`),
			jq.Match(`.spec.template.spec.containers[0].ports[] | select(.name == "http") | .containerPort == %d`, kubeAuthProxyHTTPPort),
			jq.Match(`.spec.template.spec.containers[0].ports[] | select(.name == "https") | .containerPort == %d`, kubeAuthProxyHTTPSPort),
			jq.Match(`.spec.template.spec.containers[0].ports[] | select(.name == "metrics") | .containerPort == %d`, kubeAuthProxyMetricsPort),

			// env from secret
			jq.Match(`.spec.template.spec.containers[0].env | length == 4`),
			jq.Match(`.spec.template.spec.containers[0].env[] | select(.name == "%s") | .valueFrom.secretKeyRef.name == "%s"`, gateway.EnvClientID, kubeAuthProxyCredsName),
			jq.Match(`.spec.template.spec.containers[0].env[] | select(.name == "%s") | .valueFrom.secretKeyRef.name == "%s"`, gateway.EnvClientSecret, kubeAuthProxyCredsName),
			jq.Match(`.spec.template.spec.containers[0].env[] | select(.name == "%s") | .valueFrom.secretKeyRef.name == "%s"`, gateway.EnvCookieSecret, kubeAuthProxyCredsName),
			jq.Match(`.spec.template.spec.containers[0].env[] | select(.name == "PROXY_MODE") | .value == "auth"`),

			// TLS volume mount
			jq.Match(`.spec.template.spec.containers[0].volumeMounts[] | select(.name == "tls-certs") | .mountPath == "/etc/tls/private"`),
			jq.Match(`.spec.template.spec.containers[0].volumeMounts[] | select(.name == "tls-certs") | .readOnly == true`),
			jq.Match(`.spec.template.spec.volumes[] | select(.name == "tls-certs") | .secret.secretName == "%s"`, kubeAuthProxyTLSName),

			// critical args and behavior
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--provider=openshift")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--scope=user:full")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "%s")`, expectedRedirectURL),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "%s")`, expectedCookieDomain),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--https-address=0.0.0.0:%d")`, kubeAuthProxyHTTPSPort),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--http-address=0.0.0.0:%d")`, kubeAuthProxyHTTPPort),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--tls-cert-file=/etc/tls/private/tls.crt")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--tls-key-file=/etc/tls/private/tls.key")`),

			// cookie config and related flags
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--cookie-secure=true")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--cookie-httponly=true")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--cookie-samesite=lax")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--cookie-name=_oauth2_proxy")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--cookie-expire=24h")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--cookie-refresh=1h")`),

			// auth proxy behavior flags
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--skip-provider-button")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--skip-jwt-bearer-tokens=true")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--pass-access-token=true")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--set-xauthrequest=true")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--email-domain=*")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--upstream=static://200")`),

			// metrics and trust store
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--metrics-address=0.0.0.0:%d")`, kubeAuthProxyMetricsPort),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--use-system-trust-store=true")`),

			// secret hash annotation
			jq.Match(`.spec.template.metadata.annotations["opendatahub.io/secret-hash"] != null`),
			jq.Match(`.spec.template.metadata.annotations["opendatahub.io/secret-hash"] | test("^[0-9a-f]{64}$|^$")`),
		)),
		WithCustomErrorMsg("kube-auth-proxy deployment should exist with correct configuration"),
	)

	// wait for deployment readiness using TestContext helper
	tc.EnsureDeploymentReady(types.NamespacedName{Name: kubeAuthProxyName, Namespace: gatewayNamespace}, 1)

	// kube-auth-proxy service
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Service, types.NamespacedName{
			Name:      kubeAuthProxyName,
			Namespace: gatewayNamespace,
		}),
		WithCondition(And(
			jq.Match(`.spec.selector.app == "%s"`, kubeAuthProxyName),
			jq.Match(`.spec.ports | length == 2`),
			jq.Match(`.spec.ports[] | select(.name == "https") | .port == %d`, kubeAuthProxyHTTPSPort),
			jq.Match(`.spec.ports[] | select(.name == "https") | .targetPort == %d`, kubeAuthProxyHTTPSPort),
			jq.Match(`.spec.ports[] | select(.name == "metrics") | .port == %d`, kubeAuthProxyMetricsPort),
			jq.Match(`.metadata.annotations."service.beta.openshift.io/serving-cert-secret-name" == "%s"`, kubeAuthProxyTLSName),
		)),
		WithCustomErrorMsg("kube-auth-proxy service should exist with HTTPS and metrics ports, and service-ca annotation"),
	)

	// TLS secret for auth proxy
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Secret, types.NamespacedName{
			Name:      kubeAuthProxyTLSName,
			Namespace: gatewayNamespace,
		}),
		WithCustomErrorMsg("kube-auth-proxy TLS secret should exist"),
	)

	t.Log("kube-auth-proxy deployment and service validation completed")
}

// ValidateOAuthCallbackRoute validates the OAuth callback HTTPRoute configuration.
func (tc *GatewayTestCtx) ValidateOAuthCallbackRoute(t *testing.T) {
	t.Helper()
	t.Log("Validating OAuth callback HTTPRoute")

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.HTTPRoute, types.NamespacedName{
			Name:      oauthCallbackRouteName,
			Namespace: gatewayNamespace,
		}),
		WithCondition(And(
			// parent reference checks
			jq.Match(`.spec.parentRefs | length == 1`),
			jq.Match(`.spec.parentRefs[0].group == "%s"`, gwapiv1.GroupVersion.Group),
			jq.Match(`.spec.parentRefs[0].kind == "Gateway"`),
			jq.Match(`.spec.parentRefs[0].name == "%s"`, gatewayName),
			jq.Match(`.spec.parentRefs[0].namespace == "%s"`, gatewayNamespace),

			// path match checks
			jq.Match(`.spec.rules | length == 1`),
			jq.Match(`.spec.rules[0].matches | length == 1`),
			jq.Match(`.spec.rules[0].matches[0].path.type == "PathPrefix"`),
			jq.Match(`.spec.rules[0].matches[0].path.value == "%s"`, authProxyOAuth2Path),

			// backend ref to kube-auth-proxy
			jq.Match(`.spec.rules[0].backendRefs | length == 1`),
			jq.Match(`.spec.rules[0].backendRefs[0].group == ""`),
			jq.Match(`.spec.rules[0].backendRefs[0].kind == "Service"`),
			jq.Match(`.spec.rules[0].backendRefs[0].name == "%s"`, kubeAuthProxyName),
			jq.Match(`.spec.rules[0].backendRefs[0].namespace == "%s"`, gatewayNamespace),
			jq.Match(`.spec.rules[0].backendRefs[0].port == %d`, kubeAuthProxyHTTPSPort),
			jq.Match(`.spec.rules[0].backendRefs[0].weight == 1`),

			// status
			jq.Match(`.status.parents | length > 0`),
			jq.Match(`.status.parents[0].conditions[] | select(.type == "Accepted") | .status == "True"`),
			jq.Match(`.status.parents[0].conditions[] | select(.type == "ResolvedRefs") | .status == "True"`),
		)),
		WithCustomErrorMsg("OAuth callback HTTPRoute should be properly configured and accepted"),
	)

	t.Log("OAuth callback HTTPRoute validation completed")
}

// ValidateEnvoyFilter validates the EnvoyFilter for external authorization.
func (tc *GatewayTestCtx) ValidateEnvoyFilter(t *testing.T) {
	t.Helper()
	t.Log("Validating EnvoyFilter for authentication")

	authProxyFQDN := getServiceFQDN(kubeAuthProxyName, gatewayNamespace)
	authProxyHostPort := net.JoinHostPort(authProxyFQDN, strconv.Itoa(kubeAuthProxyHTTPSPort))
	authProxyURI := "https://" + authProxyHostPort + "/oauth2/auth"
	serviceCAPath := "/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt"

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.EnvoyFilter, types.NamespacedName{
			Name:      envoyFilterName,
			Namespace: gatewayNamespace,
		}),
		WithCondition(And(
			// workload selector
			jq.Match(`.spec.workloadSelector.labels."gateway.networking.k8s.io/gateway-name" == "%s"`, gatewayName),

			// config patches length
			jq.Match(`.spec.configPatches | length == 3`),

			// Patch 1: ext_authz
			jq.Match(`.spec.configPatches[0].applyTo == "HTTP_FILTER"`),
			jq.Match(`.spec.configPatches[0].match.context == "GATEWAY"`),
			jq.Match(`.spec.configPatches[0].patch.operation == "INSERT_BEFORE"`),
			jq.Match(`.spec.configPatches[0].patch.value.name == "envoy.filters.http.ext_authz"`),

			// ext_authz config - server/uri and timeout
			jq.Match(`.spec.configPatches[0].patch.value.typed_config.http_service.server_uri.cluster == "%s"`, kubeAuthProxyName),
			jq.Match(`.spec.configPatches[0].patch.value.typed_config.http_service.server_uri.timeout == "5s"`),
			jq.Match(`.spec.configPatches[0].patch.value.typed_config.http_service.server_uri.uri == "%s"`, authProxyURI),

			// ext_authz allowed headers
			jq.Match(`.spec.configPatches[0].patch.value.typed_config.http_service.authorization_request.allowed_headers.patterns[0].exact == "cookie"`),
			jq.Match(`.spec.configPatches[0].patch.value.typed_config.http_service.authorization_response.allowed_client_headers.patterns[0].exact == "set-cookie"`),
			jq.Match(`.spec.configPatches[0].patch.value.typed_config.http_service.authorization_response.allowed_upstream_headers.patterns | any(.exact == "x-auth-request-user")`),
			jq.Match(`.spec.configPatches[0].patch.value.typed_config.http_service.authorization_response.allowed_upstream_headers.patterns | any(.exact == "x-auth-request-email")`),
			jq.Match(`.spec.configPatches[0].patch.value.typed_config.http_service.authorization_response.allowed_upstream_headers.patterns | any(.exact == "x-auth-request-access-token")`),

			// Patch 2: Lua filter token forwarding
			jq.Match(`.spec.configPatches[1].applyTo == "HTTP_FILTER"`),
			jq.Match(`.spec.configPatches[1].patch.value.name == "envoy.lua"`),
			jq.Match(`.spec.configPatches[1].patch.value.typed_config.inline_code | contains("x-auth-request-access-token")`),
			jq.Match(`.spec.configPatches[1].patch.value.typed_config.inline_code | contains("Bearer")`),
			jq.Match(`.spec.configPatches[1].patch.value.typed_config.inline_code | contains("authorization")`),

			// Patch 3: Cluster for kube-auth-proxy
			jq.Match(`.spec.configPatches[2].applyTo == "CLUSTER"`),
			jq.Match(`.spec.configPatches[2].match.context == "GATEWAY"`),
			jq.Match(`.spec.configPatches[2].patch.operation == "ADD"`),
			jq.Match(`.spec.configPatches[2].patch.value.name == "%s"`, kubeAuthProxyName),
			jq.Match(`.spec.configPatches[2].patch.value.type == "STRICT_DNS"`),
			jq.Match(`.spec.configPatches[2].patch.value.connect_timeout == "5s"`),

			// cluster endpoints
			jq.Match(`.spec.configPatches[2].patch.value.load_assignment.cluster_name == "%s"`, kubeAuthProxyName),
			jq.Match(`.spec.configPatches[2].patch.value.load_assignment.endpoints | length == 1`),
			jq.Match(`.spec.configPatches[2].patch.value.load_assignment.endpoints[0].lb_endpoints | length == 1`),
			jq.Match(`.spec.configPatches[2].patch.value.load_assignment.endpoints[0].lb_endpoints[0].endpoint.address.socket_address.address == "%s"`, authProxyFQDN),
			jq.Match(`.spec.configPatches[2].patch.value.load_assignment.endpoints[0].lb_endpoints[0].endpoint.address.socket_address.port_value == %d`, kubeAuthProxyHTTPSPort),

			// TLS config for cluster
			jq.Match(`.spec.configPatches[2].patch.value.transport_socket.name == "envoy.transport_sockets.tls"`),
			jq.Match(`.spec.configPatches[2].patch.value.transport_socket.typed_config."@type" == "type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.UpstreamTlsContext"`),
			jq.Match(`.spec.configPatches[2].patch.value.transport_socket.typed_config.common_tls_context.validation_context.trusted_ca.filename == "%s"`, serviceCAPath),
			jq.Match(`.spec.configPatches[2].patch.value.transport_socket.typed_config.sni == "%s"`, authProxyFQDN),
		)),
		WithCustomErrorMsg("EnvoyFilter should be properly configured for authentication"),
	)

	t.Log("EnvoyFilter validation completed")
}

// ValidateGatewayReadyStatus validates Gateway resource is fully operational and ready to route traffic.
func (tc *GatewayTestCtx) ValidateGatewayReadyStatus(t *testing.T) {
	t.Helper()
	t.Log("Validating Gateway ready status")

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.KubernetesGateway, types.NamespacedName{
			Name:      gatewayName,
			Namespace: gatewayNamespace,
		}),
		WithCondition(And(
			// Gateway-level conditions
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, string(gwapiv1.GatewayConditionAccepted), string(metav1.ConditionTrue)),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, string(gwapiv1.GatewayConditionProgrammed), string(metav1.ConditionTrue)),

			// External address exists (load balancer provisioned)
			jq.Match(`.status.addresses | length > 0`),
			jq.Match(`.status.addresses[0].type == "Hostname" or .status.addresses[0].type == "IPAddress"`),
			jq.Match(`.status.addresses[0].value | length > 0`),

			// Listener status - HTTPS listener must be ready
			jq.Match(`.status.listeners | length > 0`),
			jq.Match(`.status.listeners[] | select(.name == "https") | .attachedRoutes >= 1`),

			// Listener conditions - all must be healthy
			jq.Match(`.status.listeners[] | select(.name == "https") | .conditions[] | select(.type == "Accepted") | .status == "%s"`, string(metav1.ConditionTrue)),
			jq.Match(`.status.listeners[] | select(.name == "https") | .conditions[] | select(.type == "Conflicted") | .status == "%s"`, string(metav1.ConditionFalse)),
			jq.Match(`.status.listeners[] | select(.name == "https") | .conditions[] | select(.type == "Programmed") | .status == "%s"`, string(metav1.ConditionTrue)),
			jq.Match(`.status.listeners[] | select(.name == "https") | .conditions[] | select(.type == "ResolvedRefs") | .status == "%s"`, string(metav1.ConditionTrue)),

			// Listener supports HTTPRoute (required for routing)
			jq.Match(`.status.listeners[] | select(.name == "https") | .supportedKinds[] | select(.group == "%s") | .kind == "HTTPRoute"`, gwapiv1.GroupVersion.Group),
		)),
		WithCustomErrorMsg("Gateway should be fully operational with healthy listener and load balancer"),
	)

	t.Log("Gateway ready status validation completed")
}

// ValidateUnauthenticatedRedirect tests that unauthenticated requests are redirected to OAuth login.
//
// This test validates end-to-end authentication by:
// 1. Temporarily enabling Dashboard component (provides an HTTPRoute to test against)
// 2. Making an unauthenticated HTTP request to the dashboard through the gateway
// 3. Verifying the response is a redirect (302/307) to the OAuth provider
// 4. Checking the redirect URL contains OAuth authorization endpoint and callback parameters
// 5. Cleaning up by removing Dashboard component
//
// Note: Dashboard is used as a test target because it automatically creates an HTTPRoute
// that is attached to the Gateway, providing a real route to test authentication against.
func (tc *GatewayTestCtx) ValidateUnauthenticatedRedirect(t *testing.T) {
	t.Helper()

	tc.UpdateComponentStateInDataScienceClusterWithKind(operatorv1.Managed, componentApi.DashboardKind)
	defer tc.UpdateComponentStateInDataScienceClusterWithKind(operatorv1.Removed, componentApi.DashboardKind)

	tc.waitForDashboardHTTPRoute(t)
	dashboardURL := tc.getDashboardURL(t)

	tc.testUnauthenticatedAccess(t, dashboardURL)
}

// waitForDashboardHTTPRoute waits for dashboard HTTPRoute to be accepted by the Gateway.
// Note: Deployment readiness is already validated by UpdateComponentStateInDataScienceClusterWithKind
// via the DashboardReady condition in DSC, which checks deployment status via deployments.NewAction().
func (tc *GatewayTestCtx) waitForDashboardHTTPRoute(t *testing.T) {
	t.Helper()

	dashboardNamespace := tc.AppsNamespace
	dashboardRouteName := "rhods-dashboard"

	t.Log("Waiting for dashboard HTTPRoute to be accepted by Gateway")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.HTTPRoute, types.NamespacedName{
			Name:      dashboardRouteName,
			Namespace: dashboardNamespace,
		}),
		WithCondition(And(
			jq.Match(`.spec.parentRefs[] | select(.name == "%s") | .namespace == "%s"`, gatewayName, gatewayNamespace),
			jq.Match(`.status.parents[0].conditions[] | select(.type == "Accepted") | .status == "True"`),
			jq.Match(`.status.parents[0].conditions[] | select(.type == "ResolvedRefs") | .status == "True"`),
		)),
		WithCustomErrorMsg("Dashboard HTTPRoute should be accepted by Gateway"),
	)
	t.Log("Dashboard HTTPRoute is accepted")
}

// getDashboardURL returns the dashboard URL through the gateway.
func (tc *GatewayTestCtx) getDashboardURL(t *testing.T) string {
	t.Helper()

	gatewayHostname := tc.getExpectedGatewayHostname(t)
	return fmt.Sprintf("https://%s", gatewayHostname)
}

// testUnauthenticatedAccess validates that unauthenticated requests are redirected to OAuth provider.
func (tc *GatewayTestCtx) testUnauthenticatedAccess(t *testing.T, dashboardURL string) {
	t.Helper()
	t.Log("Testing unauthenticated access to dashboard")

	httpClient := tc.createHTTPClient()

	// Create context with timeout (e.g., 30 seconds)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dashboardURL, nil)
	tc.g.Expect(err).NotTo(HaveOccurred(), "Failed to create HTTP request")

	resp, err := httpClient.Do(req)
	tc.g.Expect(err).NotTo(HaveOccurred(), "Failed to make HTTP request to dashboard")
	defer resp.Body.Close()

	// Check status code is a redirect
	tc.g.Expect(resp.StatusCode).To(Or(
		Equal(http.StatusFound),
		Equal(http.StatusTemporaryRedirect),
	), "Unauthenticated request should return redirect (302/307) got %d", resp.StatusCode)

	// Validate redirect location
	location := resp.Header.Get("Location")
	tc.g.Expect(location).NotTo(BeEmpty(), "Redirect response should have Location header")
	tc.g.Expect(location).To(Or(
		ContainSubstring("/oauth/authorize"),
		ContainSubstring("/auth"),
	), "Redirect location should be to OAuth provider, got: %s", location)
	tc.g.Expect(location).To(ContainSubstring("redirect_uri="),
		"Redirect should have redirect_uri parameter, got: %s", location)
	t.Logf("Redirect goes to OAuth provider with callback URL containing: %s", authProxyOAuth2Path)

	t.Log("Unauthenticated access correctly redirects to OAuth login")
}

func (tc *GatewayTestCtx) createHTTPClient() *http.Client {
	// cookiejar.New never errors with nil options, safe to ignore error
	jar, _ := cookiejar.New(nil)

	return &http.Client{
		Jar: jar,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				// #nosec G402 -- e2e test environment requires skipping TLS verification for self-signed certificates
				InsecureSkipVerify: true,
			},
		},
		// Don't follow redirects automatically so we can inspect the Location header
		// and verify the OAuth redirect is working correctly.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// getExpectedGatewayHostname returns the expected gateway hostname based on cluster domain.
// Result is cached to avoid multiple cluster API calls.
func (tc *GatewayTestCtx) getExpectedGatewayHostname(t *testing.T) string {
	t.Helper()
	tc.once.Do(func() {
		clusterDomain, err := cluster.GetDomain(tc.Context(), tc.Client())
		if err != nil {
			// store empty and let caller fail with require if needed
			tc.cachedGatewayHostname = ""
			return
		}
		tc.cachedGatewayHostname = gatewayName + "." + clusterDomain
	})
	if tc.cachedGatewayHostname == "" {
		require.FailNow(t, "failed to determine cluster domain to compute gateway hostname")
	}
	t.Logf("Expected gateway hostname: %s", tc.cachedGatewayHostname)
	return tc.cachedGatewayHostname
}

// getServiceFQDN returns the fully qualified domain name for a Kubernetes service.
// Used to construct service addresses for EnvoyFilter configuration.
// Format: <service-name>.<namespace>.svc.cluster.local.
func getServiceFQDN(serviceName, namespace string) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, namespace)
}
