package e2e_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
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

func gatewayTestSuite(t *testing.T) {
	t.Helper()

	ctx, err := NewTestContext(t)
	require.NoError(t, err)

	componentCtx := GatewayTestCtx{
		TestContext: ctx,
	}

	testCases := []TestCase{
		{"Validate Gateway infrastructure creation", componentCtx.ValidateGatewayInfrastructure},
		{"Validate HTTPRoute creation for oauth call back", componentCtx.ValidateHTTPRouteCreation},
		{"Validate Dashboard HTTPRoute dynamic creation", componentCtx.ValidateDashboardHTTPRouteCreation},
		{"Validate DNS Map Route creation", componentCtx.ValidateDNSMapRouteCreation},
		{"Validate EnvoyFilter creation", componentCtx.ValidateEnvoyFilterCreation},
		{"Validate DestinationRule creation", componentCtx.ValidateDestinationRuleCreation},
	}

	RunTestCases(t, testCases)
}

func (tc *GatewayTestCtx) ValidateGatewayInfrastructure(t *testing.T) {
	t.Helper()

	// Ensure GatewayConfig exists, has proper configuration, and is owned by DSCInitialization
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.GatewayConfig, types.NamespacedName{Name: serviceApi.GatewayConfigName}),
		WithCondition(And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
			jq.Match(`.metadata.ownerReferences[0].kind == "DSCInitialization"`),
			jq.Match(`.metadata.ownerReferences[0].name == "default-dsci"`),
		)),
		WithCustomErrorMsg(serviceApi.GatewayConfigName+" CR should have Ready condition with status True and be owned by default-dsci DSCInitialization"),
	)

	// Validate GatewayClass is created
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.GatewayClass, types.NamespacedName{Name: gateway.GatewayClassName}),
		WithCondition(And(
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, serviceApi.GatewayConfigKind),
			jq.Match(`.metadata.ownerReferences[0].name == "%s"`, serviceApi.GatewayConfigName),
		)),
		WithCustomErrorMsg(gateway.GatewayClassName+" should be owned by "+serviceApi.GatewayConfigName+" GatewayConfig"),
	)

	// Validate certificate secret
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Secret, types.NamespacedName{
			Name:      gateway.DefaultGatewayTLSSecretName,
			Namespace: gateway.GatewayNamespace,
		}),
		WithCondition(And(
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, serviceApi.GatewayConfigKind),
			jq.Match(`.metadata.ownerReferences[0].name == "%s"`, serviceApi.GatewayConfigName),
		)),
		WithCustomErrorMsg(gateway.DefaultGatewayTLSSecretName+" secret should be owned by "+serviceApi.GatewayConfigName+" GatewayConfig"),
	)

	// Validate Gateway API resource with configuration, status, and ownership
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.KubernetesGateway, types.NamespacedName{
			Name:      gateway.DefaultGatewayName,
			Namespace: gateway.GatewayNamespace,
		}),
		WithCondition(And(
			jq.Match(`.spec.gatewayClassName == "%s"`, gateway.GatewayClassName),
			jq.Match(`.spec.listeners | length > 0`),
			jq.Match(`.spec.listeners[] | select(.name == "https") | .protocol == "%s"`, string(gwapiv1.HTTPSProtocolType)),
			jq.Match(`.spec.listeners[] | select(.name == "https") | .port == %d`, gateway.StandardHTTPSPort),
			jq.Match(`.spec.listeners[] | select(.name == "https") | .tls.certificateRefs[0].name == "%s"`, gateway.DefaultGatewayTLSSecretName),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, string(gwapiv1.GatewayConditionAccepted), "True"),
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, serviceApi.GatewayConfigKind),
			jq.Match(`.metadata.ownerReferences[0].name == "%s"`, serviceApi.GatewayConfigName),
		)),
		WithCustomErrorMsg(gateway.DefaultGatewayName+" should be properly configured, accepted by the gatewayconfig, and owned by "+serviceApi.GatewayConfigName+" GatewayConfig"),
	)

	// Validate auth proxy resources created by templates
	// Validate kube-auth-proxy deployment
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      gateway.KubeAuthProxyName,
			Namespace: gateway.GatewayNamespace,
		}),
		WithCondition(And(
			jq.Match(`.spec.template.spec.containers[0].ports[] | select(.name == "http") | .containerPort == %d`, gateway.AuthProxyHTTPPort),
			jq.Match(`.spec.template.spec.containers[0].ports[] | select(.name == "https") | .containerPort == %d`, gateway.GatewayHTTPSPort),
			jq.Match(`.spec.template.spec.containers[0].ports[] | select(.name == "metrics") | .containerPort == %d`, gateway.AuthProxyMetricsPort),
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, serviceApi.GatewayConfigKind),
			jq.Match(`.metadata.ownerReferences[0].name == "%s"`, serviceApi.GatewayConfigName),
		)),
		WithCustomErrorMsg(gateway.KubeAuthProxyName+" deployment should be created with correct ports and owned by "+serviceApi.GatewayConfigName+" GatewayConfig"),
	)

	// Validate kube-auth-proxy service
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Service, types.NamespacedName{
			Name:      gateway.KubeAuthProxyName,
			Namespace: gateway.GatewayNamespace,
		}),
		WithCondition(And(
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, serviceApi.GatewayConfigKind),
			jq.Match(`.metadata.ownerReferences[0].name == "%s"`, serviceApi.GatewayConfigName),
		)),
		WithCustomErrorMsg(gateway.KubeAuthProxyName+" service should be created and owned by "+
			serviceApi.GatewayConfigName+" GatewayConfig"),
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
				jq.Match(`.redirectURIs[0] | contains("%s/callback")`, gateway.AuthProxyOAuth2Path),
				jq.Match(`.secret != null and .secret != ""`),
				jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, serviceApi.GatewayConfigKind),
				jq.Match(`.metadata.ownerReferences[0].name == "%s"`, serviceApi.GatewayConfigName),
			)),
			WithCustomErrorMsg(gateway.AuthClientID+" OAuthClient should be created with correct OAuth configuration and owned by "+serviceApi.GatewayConfigName+" GatewayConfig"),
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
			jq.Match(`.spec.parentRefs[0].name == "%s"`, gateway.DefaultGatewayName),
			jq.Match(`.spec.parentRefs[0].namespace == "%s"`, gateway.GatewayNamespace),
			jq.Match(`.spec.rules[0].matches[0].path.value == "%s"`, gateway.AuthProxyOAuth2Path),
			jq.Match(`.spec.rules[0].backendRefs[0].name == "%s"`, gateway.KubeAuthProxyName),
			jq.Match(`.spec.rules[0].backendRefs[0].port == %d`, gateway.GatewayHTTPSPort),
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, serviceApi.GatewayConfigKind),
			jq.Match(`.metadata.ownerReferences[0].name == "%s"`, serviceApi.GatewayConfigName),
		)),
		WithCustomErrorMsg(gateway.OAuthCallbackRouteName+" HTTPRoute should be created with correct configuration and owned by "+
			serviceApi.GatewayConfigName+" GatewayConfig"),
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
			jq.Match(`.spec.workloadSelector.labels["gateway.networking.k8s.io/gateway-name"] == "%s"`, gateway.DefaultGatewayName),
			jq.Match(`.spec.configPatches | length > 0`),
			jq.Match(`.spec.configPatches[] | select(.applyTo == "HTTP_FILTER")`),
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, serviceApi.GatewayConfigKind),
			jq.Match(`.metadata.ownerReferences[0].name == "%s"`, serviceApi.GatewayConfigName),
		)),
		WithCustomErrorMsg(gateway.AuthnFilterName+" EnvoyFilter should be created with correct authentication configuration and owned by "+
			serviceApi.GatewayConfigName+" GatewayConfig"),
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
			jq.Match(`.spec.host == "%s.%s.svc.cluster.local"`, gateway.KubeAuthProxyName, gateway.GatewayNamespace),
			jq.Match(`.spec.trafficPolicy.portLevelSettings | length > 0`),
			jq.Match(`.spec.trafficPolicy.portLevelSettings[] | select(.port.number == %d) | .tls.mode == "SIMPLE"`, gateway.GatewayHTTPSPort),
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, serviceApi.GatewayConfigKind),
			jq.Match(`.metadata.ownerReferences[0].name == "%s"`, serviceApi.GatewayConfigName),
		)),
		WithCustomErrorMsg(gateway.DestinationRuleName+" DestinationRule should be created with correct TLS configuration and owned by "+serviceApi.GatewayConfigName+" GatewayConfig"),
	)

	t.Log("DestinationRule validation completed successfully")
}

func (tc *GatewayTestCtx) ValidateDashboardHTTPRouteCreation(t *testing.T) {
	t.Helper()

	// Check if Dashboard CR exists - if it does, HTTPRoute should be created
	dashboardExists := tc.checkDashboardCRExists(t)

	if dashboardExists {
		// Validate Dashboard HTTPRoute is created when Dashboard CR exists
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.HTTPRoute, types.NamespacedName{
				Name:      "default-dashboard-route",
				Namespace: tc.AppsNamespace,
			}),
			WithCondition(And(
				jq.Match(`.spec.parentRefs[0].name == "%s"`, gateway.DefaultGatewayName),
				jq.Match(`.spec.parentRefs[0].namespace == "%s"`, gateway.GatewayNamespace),
				jq.Match(`.spec.rules[0].matches[0].path.value == "/"`),
				jq.Match(`.spec.rules[0].backendRefs[0].name == "odh-dashboard"`),
				jq.Match(`.spec.rules[0].backendRefs[0].port == %d`, gateway.GatewayHTTPSPort),
				jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, serviceApi.GatewayConfigKind),
				jq.Match(`.metadata.ownerReferences[0].name == "%s"`, serviceApi.GatewayConfigName),
			)),
			WithCustomErrorMsg("default-dashboard-route HTTPRoute should be created when Dashboard CR exists and owned by "+serviceApi.GatewayConfigName+" GatewayConfig"),
		)
		t.Log("Dashboard HTTPRoute validation completed successfully - Dashboard CR exists")
	} else {
		// Validate Dashboard HTTPRoute is NOT created when Dashboard CR doesn't exist
		tc.EnsureResourceDoesNotExist(
			WithMinimalObject(gvk.HTTPRoute, types.NamespacedName{
				Name:      "default-dashboard-route",
				Namespace: tc.AppsNamespace,
			}),
			WithCustomErrorMsg("default-dashboard-route HTTPRoute should NOT be created when Dashboard CR doesn't exist"),
		)
		t.Log("Dashboard HTTPRoute validation completed successfully - Dashboard CR doesn't exist, HTTPRoute not created")
	}
}

func (tc *GatewayTestCtx) ValidateDNSMapRouteCreation(t *testing.T) {
	t.Helper()

	// Validate DNS Map Route (OpenShift Route) is created
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Route, types.NamespacedName{
			Name:      gateway.DefaultGatewayName,
			Namespace: gateway.GatewayNamespace,
		}),
		WithCondition(And(
			jq.Match(`.spec.port.targetPort == "https"`),
			jq.Match(`.spec.tls.termination == "passthrough"`),
			jq.Match(`.spec.tls.insecureEdgeTerminationPolicy == "Redirect"`),
			jq.Match(`.spec.to.kind == "Service"`),
			jq.Match(`.spec.to.name | contains("%s")`, gateway.DefaultGatewayName),
			jq.Match(`.spec.wildcardPolicy == "None"`),
		)),
		WithCustomErrorMsg(gateway.DefaultGatewayName+" Route should be created with correct DNS mapping configuration"),
	)

	t.Log("DNS Map Route validation completed successfully")
}

// Helper function to check if Dashboard CR exists.
func (tc *GatewayTestCtx) checkDashboardCRExists(t *testing.T) bool {
	t.Helper()

	dashboardCR := &componentApi.Dashboard{}
	dashboardCR.Name = componentApi.DashboardInstanceName

	err := tc.Client().Get(tc.Context(), client.ObjectKeyFromObject(dashboardCR), dashboardCR)
	return err == nil
}
