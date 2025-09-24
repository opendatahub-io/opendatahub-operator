package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

const (
	gatewayConfigName  = "default-gateway"
	gatewayName        = "data-science-gateway"
	gatewayClassName   = "data-science-gateway-class"
	gatewayNamespace   = "openshift-ingress"
	kubeAuthProxyName  = "kube-auth-proxy"
	oauthCallbackRoute = "oauth-callback-route"
)

type GatewayTestCtx struct {
	*TestContext
}

func gatewayTestSuite(t *testing.T) { //nolint:unused
	t.Helper()

	ctx, err := NewTestContext(t)
	require.NoError(t, err)

	componentCtx := GatewayTestCtx{
		TestContext: ctx,
	}

	testCases := []TestCase{
		{"Validate Gateway infrastructure creation", componentCtx.ValidateGatewayInfrastructure},
		{"Validate HTTPRoute creation for oauth call back", componentCtx.ValidateHTTPRouteCreation},
		{"Validate EnvoyFilter creation", componentCtx.ValidateEnvoyFilterCreation},
	}

	RunTestCases(t, testCases)
}

func (tc *GatewayTestCtx) ValidateGatewayInfrastructure(t *testing.T) {
	t.Helper()

	t.Log("Validating Gateway service and API resources creation")

	// First ensure GatewayConfig exists and has proper configuration
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.GatewayConfig, types.NamespacedName{Name: gatewayConfigName}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue)),
		WithCustomErrorMsg("GatewayConfig CR should have Ready condition with status True"),
	)

	// Validate GatewayClass is created
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.GatewayClass, types.NamespacedName{Name: gatewayClassName}),
	)

	// Validate certificate secret
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Secret, types.NamespacedName{
			Name:      serviceApi.DefaultGatewayTLSSecretName,
			Namespace: gatewayNamespace,
		}),
	)

	// Validate Gateway API resource with configuration and status
	t.Log("Validating Gateway API spec and status")
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
			jq.Match(`.spec.listeners[] | select(.name == "https") | .tls.certificateRefs[0].name == "%s"`, serviceApi.DefaultGatewayTLSSecretName),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, string(gwapiv1.GatewayConditionAccepted), "True"),
		)),
		WithCustomErrorMsg("Gateway should be properly configured and accepted by the gatewayconfig"),
	)

	// Validate auth proxy resources created by templates
	t.Log("Validating auth proxy deployment, service and HTTPRoute resources")

	// Validate kube-auth-proxy deployment
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      kubeAuthProxyName,
			Namespace: gatewayNamespace,
		}),
		WithCustomErrorMsg("kube-auth-proxy deployment should be created"),
	)

	// Validate kube-auth-proxy service
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Service, types.NamespacedName{
			Name:      kubeAuthProxyName,
			Namespace: gatewayNamespace,
		}),
		WithCustomErrorMsg("kube-auth-proxy service should be created"),
	)

	t.Log("Gateway API resources validation completed successfully")
}

func (tc *GatewayTestCtx) ValidateHTTPRouteCreation(t *testing.T) {
	t.Helper()

	t.Log("Validating HTTPRoute creation for OAuth callback")

	// Validate kube-auth-proxy HTTPRoute
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.HTTPRoute, types.NamespacedName{
			Name:      oauthCallbackRoute,
			Namespace: gatewayNamespace,
		}),
		WithCondition(And(
			jq.Match(`.spec.parentRefs[0].name == "%s"`, gatewayName),
			jq.Match(`.spec.parentRefs[0].namespace == "%s"`, gatewayNamespace),
			jq.Match(`.spec.rules[0].matches[0].path.value == "/oauth2"`),
			jq.Match(`.spec.rules[0].backendRefs[0].name == "%s"`, kubeAuthProxyName),
			jq.Match(`.spec.rules[0].backendRefs[0].port == 8443`), // TODO: if we make this port change we better use variable.
		)),
		WithCustomErrorMsg("oauth-callback-route HTTPRoute should be created with correct configuration"),
	)

	t.Log("HTTPRoute validation completed successfully")
}

func (tc *GatewayTestCtx) ValidateEnvoyFilterCreation(t *testing.T) {
	t.Helper()

	t.Log("Validating EnvoyFilter creation for authentication")

	// Validate EnvoyFilter for authentication
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.EnvoyFilter, types.NamespacedName{
			Name:      "authn-filter",
			Namespace: gatewayNamespace,
		}),
		WithCondition(And(
			jq.Match(`.spec.workloadSelector.labels["gateway.networking.k8s.io/gateway-name"] == "%s"`, gatewayName),
			jq.Match(`.spec.configPatches | length > 0`),
			jq.Match(`.spec.configPatches[] | select(.applyTo == "HTTP_FILTER")`),
		)),
		WithCustomErrorMsg("authn-filter EnvoyFilter should be created with correct authentication configuration"),
	)

	t.Log("EnvoyFilter validation completed successfully")
}
