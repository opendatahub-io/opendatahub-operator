package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
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
		{"Validate DestinationRule creation", componentCtx.ValidateDestinationRuleCreation},
	}

	RunTestCases(t, testCases)
}

func (tc *GatewayTestCtx) ValidateGatewayInfrastructure(t *testing.T) {
	t.Helper()

	// First ensure GatewayConfig exists and has proper configuration
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.GatewayConfig, types.NamespacedName{Name: serviceApi.GatewayConfigName}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue)),
		WithCustomErrorMsg(serviceApi.GatewayConfigName+" CR should have Ready condition with status True"),
	)

	// Validate GatewayClass is created
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.GatewayClass, types.NamespacedName{Name: gateway.GatewayClassName}),
	)

	// Validate certificate secret
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Secret, types.NamespacedName{
			Name:      gateway.DefaultGatewayTLSSecretName,
			Namespace: gateway.GatewayNamespace,
		}),
	)

	// Validate Gateway API resource with configuration and status
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.KubernetesGateway, types.NamespacedName{
			Name:      gateway.GatewayName,
			Namespace: gateway.GatewayNamespace,
		}),
		WithCondition(And(
			jq.Match(`.spec.gatewayClassName == "%s"`, gateway.GatewayClassName),
			jq.Match(`.spec.listeners | length > 0`),
			jq.Match(`.spec.listeners[] | select(.name == "https") | .protocol == "%s"`, string(gwapiv1.HTTPSProtocolType)),
			jq.Match(`.spec.listeners[] | select(.name == "https") | .port == 443`),
			jq.Match(`.spec.listeners[] | select(.name == "https") | .tls.certificateRefs[0].name == "%s"`, gateway.DefaultGatewayTLSSecretName),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, string(gwapiv1.GatewayConditionAccepted), "True"),
		)),
		WithCustomErrorMsg(gateway.GatewayName+" should be properly configured and accepted by the gatewayconfig"),
	)

	// Validate auth proxy resources created by templates
	// Validate kube-auth-proxy deployment
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      gateway.KubeAuthProxyName,
			Namespace: gateway.GatewayNamespace,
		}),
		WithCustomErrorMsg(gateway.KubeAuthProxyName+" deployment should be created"),
	)

	// Validate kube-auth-proxy service
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Service, types.NamespacedName{
			Name:      gateway.KubeAuthProxyName,
			Namespace: gateway.GatewayNamespace,
		}),
		WithCustomErrorMsg(gateway.KubeAuthProxyName+" service should be created"),
	)

	// Validate OAuthClient only if in IntegratedOAuth mode
	if isOAuth, err := cluster.IsIntegratedOAuth(tc.Context(), tc.Client()); err != nil {
		t.Fatalf("Failed to get cluster authentication config: %v", err)
	} else if isOAuth {
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.OAuthClient, types.NamespacedName{
				Name: gateway.AuthClientID,
			}),
			WithCondition(And(
				jq.Match(`.grantMethod == "auto"`),
				jq.Match(`.redirectURIs | length > 0`),
				jq.Match(`.redirectURIs[0] | contains("/oauth2/callback")`),
				jq.Match(`.secret != null and .secret != ""`),
			)),
			WithCustomErrorMsg(gateway.AuthClientID+" OAuthClient should be created with correct OAuth configuration"),
		)
	}

	t.Log("Gateway infrastructure resources validation completed successfully")
}

func (tc *GatewayTestCtx) ValidateHTTPRouteCreation(t *testing.T) {
	t.Helper()
	// Validate kube-auth-proxy HTTPRoute
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.HTTPRoute, types.NamespacedName{
			Name:      gateway.OAuthCallbackRouteName,
			Namespace: gateway.GatewayNamespace,
		}),
		WithCondition(And(
			jq.Match(`.spec.parentRefs[0].name == "%s"`, gateway.GatewayName),
			jq.Match(`.spec.parentRefs[0].namespace == "%s"`, gateway.GatewayNamespace),
			jq.Match(`.spec.rules[0].matches[0].path.value == "/oauth2"`),
			jq.Match(`.spec.rules[0].backendRefs[0].name == "%s"`, gateway.KubeAuthProxyName),
			jq.Match(`.spec.rules[0].backendRefs[0].port == 8443`), // TODO: if we make this port change we better use variable.
		)),
		WithCustomErrorMsg(gateway.OAuthCallbackRouteName+" HTTPRoute should be created with correct configuration"),
	)

	t.Log("HTTPRoute validation completed successfully")
}

func (tc *GatewayTestCtx) ValidateEnvoyFilterCreation(t *testing.T) {
	t.Helper()

	// Validate EnvoyFilter for authentication
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.EnvoyFilter, types.NamespacedName{
			Name:      gateway.AuthnFilterName,
			Namespace: gateway.GatewayNamespace,
		}),
		WithCondition(And(
			jq.Match(`.spec.workloadSelector.labels["gateway.networking.k8s.io/gateway-name"] == "%s"`, gateway.GatewayName),
			jq.Match(`.spec.configPatches | length > 0`),
			jq.Match(`.spec.configPatches[] | select(.applyTo == "HTTP_FILTER")`),
		)),
		WithCustomErrorMsg(gateway.AuthnFilterName+" EnvoyFilter should be created with correct authentication configuration"),
	)

	t.Log("EnvoyFilter validation completed successfully")
}

func (tc *GatewayTestCtx) ValidateDestinationRuleCreation(t *testing.T) {
	t.Helper()

	// Validate DestinationRule for TLS
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DestinationRule, types.NamespacedName{
			Name:      gateway.DestinationRuleName,
			Namespace: gateway.GatewayNamespace,
		}),
		WithCondition(And(
			jq.Match(`.spec.host == "*"`),
			jq.Match(`.spec.trafficPolicy.portLevelSettings | length > 0`),
			jq.Match(`.spec.trafficPolicy.portLevelSettings[] | select(.port.number == 8443) | .tls.mode == "SIMPLE"`),
			jq.Match(`.spec.trafficPolicy.portLevelSettings[] | select(.port.number == 443) | .tls.mode == "SIMPLE"`),
		)),
		WithCustomErrorMsg(gateway.DestinationRuleName+" DestinationRule should be created with correct TLS configuration"),
	)

	t.Log("DestinationRule validation completed successfully")
}
