package e2e_test

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	gomegaTypes "github.com/onsi/gomega/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency/certmanager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

// Gateway TLS and EnvoyFilter configuration constants.
const (
	gatewayTLSSecretName        = "data-science-gatewayconfig-tls"
	gatewayServiceTLSSecretName = gateway.GatewayServiceTLSSecretName
	envoyFilterName             = "data-science-authn-filter"
	expectedSecretDataKeys      = 3
	SecretHashAnnotation        = "opendatahub.io/secret-hash"
)

// Gateway infrastructure and OAuth proxy configuration constants.
// These match the values defined in internal/controller/services/gateway package.
const (
	gatewayConfigName          = serviceApi.GatewayConfigName
	gatewaySubdomain           = gateway.DefaultGatewaySubdomain
	gatewayClassName           = gateway.GatewayClassName
	defaultGatewayListenerName = gateway.DefaultGatewayListenerName
	standardHTTPSPort          = gateway.StandardHTTPSPort
	oauthClientName            = gateway.AuthClientID
	kubeAuthProxyName          = gateway.KubeAuthProxyName
	kubeAuthProxyTLSName       = gateway.KubeAuthProxyTLSName
	kubeAuthProxyCredsName     = gateway.KubeAuthProxySecretsName
	oauthCallbackRouteName     = gateway.OAuthCallbackRouteName
	authProxyOAuth2Path        = gateway.AuthProxyOAuth2Path
	kubeAuthProxyHTTPPort      = gateway.AuthProxyHTTPPort
	kubeAuthProxyHTTPSPort     = gateway.GatewayHTTPSPort
	kubeAuthProxyMetricsPort   = gateway.AuthProxyMetricsPort
)

type GatewayTestCtx struct {
	*TestContext

	// cachedGatewayHostname stores the computed gateway hostname to avoid repeated cluster API calls.
	cachedGatewayHostname string
	// cachedIngressMode stores the detected ingress mode.
	cachedIngressMode serviceApi.IngressMode
	// cachedOIDCConfig stores the OIDC config from GatewayConfig (nil if not BYOIDC).
	cachedOIDCConfig *serviceApi.OIDCConfig
	// once ensures thread-safe lazy initialization of cachedGatewayHostname.
	once sync.Once
	// ingressModeOnce ensures thread-safe lazy initialization of cachedIngressMode.
	ingressModeOnce sync.Once
	// oidcConfigOnce ensures thread-safe lazy initialization of cachedOIDCConfig.
	oidcConfigOnce sync.Once
}

func (tc *GatewayTestCtx) gatewayName() string {
	return gateway.GetDefaultGatewayName()
}

func (tc *GatewayTestCtx) gatewayNamespace() string {
	return gateway.GetGatewayNamespace()
}

func (tc *GatewayTestCtx) gatewayControllerName() string {
	return string(gateway.GetGatewayControllerName())
}

func (tc *GatewayTestCtx) getGateway(ctx context.Context) (*gwapiv1.Gateway, error) {
	current := &gwapiv1.Gateway{}
	err := tc.Client().Get(ctx, types.NamespacedName{
		Name:      tc.gatewayName(),
		Namespace: tc.gatewayNamespace(),
	}, current)
	return current, err
}

func gatewayTestSuite(t *testing.T) {
	t.Helper()

	ctx, err := NewTestContext(t)
	require.NoError(t, err)

	gatewayCtx := &GatewayTestCtx{
		TestContext: ctx,
	}

	// Define test cases.
	testCases := []TestCase{
		{"Validate GatewayConfig creation", gatewayCtx.ValidateGatewayConfig},
		{"Validate Gateway infrastructure", gatewayCtx.ValidateGatewayInfrastructure},
		{"Validate additional Gateways", gatewayCtx.ValidateAdditionalGateways},
		{"Validate XKS cert-manager certificates", gatewayCtx.ValidateXKSCertManagerCertificates},
		{"Validate XKS certificate readiness recovery", gatewayCtx.ValidateXKSCertificateReadinessRecovery},
		// IntegratedOAuth-specific tests (skipped on BYOIDC)
		{"Validate OAuth client and secret creation", gatewayCtx.ValidateOAuthClientAndSecret},
		{"Validate authentication proxy deployment", gatewayCtx.ValidateAuthProxyDeployment},
		{"Validate unauthenticated access redirects to login", gatewayCtx.ValidateUnauthenticatedRedirect},
		// BYOIDC-specific tests (skipped on IntegratedOAuth)
		{"Validate OIDC proxy secret creation", gatewayCtx.ValidateOIDCProxySecret},
		{"Validate OIDC authentication proxy deployment", gatewayCtx.ValidateOIDCAuthProxyDeployment},
		{"Validate OIDC token forwarding to dashboard", gatewayCtx.ValidateOIDCTokenForwarding},
		{"Validate OIDC unauthenticated access redirects to login", gatewayCtx.ValidateOIDCUnauthenticatedRedirect},
		// Common tests (run on both)
		{"Validate kube-auth-proxy TLS args match cluster APIServer", gatewayCtx.ValidateKubeAuthProxyTLSArgsMatchAPIServer},
		{"Validate HorizontalPodAutoscaler configuration updates", gatewayCtx.ValidateHPA},
		{"Validate NetworkPolicy creation", gatewayCtx.ValidateNetworkPolicy},
		{"Validate NetworkPolicy ingress traffic", gatewayCtx.ValidateNetworkPolicyIngressTraffic},
		{"Validate OAuth callback HTTPRoute", gatewayCtx.ValidateOAuthCallbackRoute},
		{"Validate EnvoyFilter creation", gatewayCtx.ValidateEnvoyFilter},
		{"Validate EDS endpoint discovery", gatewayCtx.ValidateEDSEndpointDiscovery},
		{"Validate Gateway ready status", gatewayCtx.ValidateGatewayReadyStatus},
		{"Validate dashboard redirect resources", gatewayCtx.DashboardRedirectTestSuite},
	}

	RunTestCases(t, testCases)
}

// makeRedirectURL constructs the OAuth redirect URL for the authentication proxy.
// Format: https://<gateway-hostname>/oauth2/callback
func makeRedirectURL(hostname string) string {
	return fmt.Sprintf("--redirect-url=https://%s%s", hostname, gateway.OAuthCallbackPath)
}

// makeCookieDomain constructs the cookie domain argument for the authentication proxy.
// Ensures OAuth cookies work across all routes on the gateway.
func makeCookieDomain(hostname string) string {
	return fmt.Sprintf("--cookie-domain=%s", hostname)
}

// ValidateGatewayConfig ensures the GatewayConfig CR exists and is properly configured.
func (tc *GatewayTestCtx) ValidateGatewayConfig(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke)
	t.Log("Validating GatewayConfig resource")

	readyCondition := jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "%s"`, metav1.ConditionTrue)

	if tc.IsXKS() {
		// On XKS, GatewayConfig is created by the Helm chart (not the operator),
		// so it has no ownerReferences. Only assert Ready status.
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.GatewayConfig, types.NamespacedName{Name: gatewayConfigName}),
			WithCondition(readyCondition),
			WithCustomErrorMsg("GatewayConfig should be Ready"),
		)
	} else {
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.GatewayConfig, types.NamespacedName{Name: gatewayConfigName}),
			WithCondition(And(
				readyCondition,
				jq.Match(`.metadata.ownerReferences[0].kind == "DSCInitialization"`),
				jq.Match(`.metadata.ownerReferences[0].name == "%s"`, tc.DSCInitializationNamespacedName.Name),
			)),
			WithCustomErrorMsg("GatewayConfig should be Ready and owned by %s DSCInitialization", tc.DSCInitializationNamespacedName.Name),
		)
	}

	t.Log("GatewayConfig validation completed")
}

// ValidateGatewayInfrastructure validates Gateway API resources (GatewayClass, Gateway, TLS).
func (tc *GatewayTestCtx) ValidateGatewayInfrastructure(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)
	t.Log("Validating Gateway infrastructure resources")

	t.Log("Validating GatewayClass resource")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.GatewayClass, types.NamespacedName{Name: gatewayClassName}),
		WithCondition(jq.Match(`.spec.controllerName == "%s"`, tc.gatewayControllerName())),
		WithCustomErrorMsg("GatewayClass should exist with expected Gateway controller"),
	)

	tlsSecretName := tc.getTLSSecretName(t)
	t.Logf("Validating TLS certificate secret: %s", tlsSecretName)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Secret, types.NamespacedName{
			Name:      tlsSecretName,
			Namespace: tc.gatewayNamespace(),
		}),
		WithCustomErrorMsg("TLS secret %s should exist", tlsSecretName),
	)

	// Gateway validation with mode-specific TLS secret reference
	t.Log("Validating Gateway resource")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.KubernetesGateway, types.NamespacedName{
			Name:      tc.gatewayName(),
			Namespace: tc.gatewayNamespace(),
		}),
		WithCondition(And(
			jq.Match(`.spec.gatewayClassName == "%s"`, gatewayClassName),
			jq.Match(`.spec.listeners[] | select(.name == "%s") | .tls.certificateRefs[0].name == "%s"`, defaultGatewayListenerName, tlsSecretName),
		)),
		WithCustomErrorMsg("Gateway should be created with correct HTTPS listener configuration"),
	)

	// OcpRoute mode: validate the OCP Route exists
	if tc.isOcpRouteMode(t) {
		tc.validateOCPRoute(t)
	}

	t.Log("Gateway infrastructure validation completed")
}

// ValidateAdditionalGateways validates one Gateway and bridge Route per additional ingress.
func (tc *GatewayTestCtx) ValidateAdditionalGateways(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier1)
	if !tc.isOcpRouteMode(t) {
		t.Skip("additional Gateways require OcpRoute mode")
	}

	g := NewWithT(t)
	ctx := tc.Context()
	gatewayConfig := &serviceApi.GatewayConfig{}
	require.NoError(t, tc.Client().Get(ctx, types.NamespacedName{Name: gatewayConfigName}, gatewayConfig))
	original := gatewayConfig.DeepCopy()
	originalGateway, err := tc.getGateway(ctx)
	require.NoError(t, err)
	dsc := &dscv2.DataScienceCluster{}
	require.NoError(t, tc.Client().Get(ctx, tc.DataScienceClusterNamespacedName, dsc))
	originalDashboardState := dsc.Spec.Components.Dashboard.ManagementState
	if originalDashboardState != operatorv1.Managed {
		tc.UpdateComponentStateInDataScienceClusterWithKind(operatorv1.Managed, componentApi.DashboardKind)
		t.Cleanup(func() {
			tc.UpdateComponentStateInDataScienceClusterWithKind(originalDashboardState, componentApi.DashboardKind)
		})
	}
	tc.waitForDashboardHTTPRoute(t)
	defaultRouteKey := types.NamespacedName{Name: tc.gatewayName(), Namespace: tc.gatewayNamespace()}
	defaultRoute := &routev1.Route{}
	require.NoError(t, tc.Client().Get(ctx, defaultRouteKey, defaultRoute))
	defaultRouteUID := defaultRoute.UID
	require.NotEmpty(t, defaultRoute.Spec.Host)
	dashboardRouteKey := types.NamespacedName{
		Name: getDashboardRouteNameByPlatform(tc.FetchPlatformRelease()), Namespace: tc.AppsNamespace,
	}
	dashboardHTTPRoute := &gwapiv1.HTTPRoute{}
	require.NoError(t, tc.Client().Get(ctx, dashboardRouteKey, dashboardHTTPRoute))
	authHTTPRouteKey := types.NamespacedName{Name: gateway.OAuthCallbackRouteName, Namespace: tc.gatewayNamespace()}
	authHTTPRoute := &gwapiv1.HTTPRoute{}
	require.NoError(t, tc.Client().Get(ctx, authHTTPRouteKey, authHTTPRoute))
	checkDefault := func() {
		tc.assertDefaultGatewayContinuity(ctx, t, originalGateway.UID, defaultRouteUID, defaultRoute.Spec.Host)
		tc.waitForDashboardHTTPRoute(t)
		currentRoute := &routev1.Route{}
		g.Expect(tc.Client().Get(ctx, defaultRouteKey, currentRoute)).To(Succeed())
		g.Expect(currentRoute.Spec).To(Equal(defaultRoute.Spec))
		currentDashboardHTTPRoute := &gwapiv1.HTTPRoute{}
		g.Expect(tc.Client().Get(ctx, dashboardRouteKey, currentDashboardHTTPRoute)).To(Succeed())
		g.Expect(currentDashboardHTTPRoute.Spec).To(Equal(dashboardHTTPRoute.Spec))
		currentAuthHTTPRoute := &gwapiv1.HTTPRoute{}
		g.Expect(tc.Client().Get(ctx, authHTTPRouteKey, currentAuthHTTPRoute)).To(Succeed())
		g.Expect(currentAuthHTTPRoute.Spec).To(Equal(authHTTPRoute.Spec))
	}
	ingressControllerName := fmt.Sprintf("e2e-alpha-shard-%x", time.Now().UnixNano())
	ingressControllerKey := types.NamespacedName{
		Name: ingressControllerName, Namespace: cluster.IngressControllerName.Namespace,
	}
	alpha := serviceApi.AdditionalIngress{
		Name:                  "e2e-alpha",
		Hostname:              ingressControllerName + ".e2e.invalid",
		IngressControllerName: ingressControllerName,
		RouteLabels:           map[string]string{"example.com/ingress": "e2e-alpha"},
	}
	beta := serviceApi.AdditionalIngress{
		Name: "e2e-beta", Hostname: "e2e-beta.example.com",
		IngressControllerName: "e2e-beta-shard",
		RouteLabels:           map[string]string{"example.com/ingress": "e2e-beta"},
	}
	alphaDomain := ingressControllerName + ".e2e.invalid"
	replicas := int32(1)
	alphaIngressController := &operatorv1.IngressController{
		ObjectMeta: metav1.ObjectMeta{Name: ingressControllerKey.Name, Namespace: ingressControllerKey.Namespace},
		Spec: operatorv1.IngressControllerSpec{
			Domain: alphaDomain, Replicas: &replicas,
			EndpointPublishingStrategy: &operatorv1.EndpointPublishingStrategy{Type: operatorv1.PrivateStrategyType},
			RouteSelector:              &metav1.LabelSelector{MatchLabels: alpha.RouteLabels},
		},
	}
	require.NoError(t, tc.Client().Create(ctx, alphaIngressController), "create temporary private IngressController")
	t.Cleanup(func() {
		restoreAdditionalIngressGatewayTest(t, tc, original, originalGateway, ingressControllerKey)
	})
	t.Cleanup(func() { tc.logAdditionalIngressEvidence(t, ingressControllerKey) })

	update := func(additional serviceApi.AdditionalIngresses) {
		g.Expect(retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			current := &serviceApi.GatewayConfig{}
			if err := tc.Client().Get(ctx, types.NamespacedName{Name: gatewayConfigName}, current); err != nil {
				return err
			}
			current.Spec.AdditionalIngresses = additional
			return tc.Client().Update(ctx, current)
		})).To(Succeed())
	}
	validateStatus := func(
		expectedCount int,
		name, hostname string,
		expectedRouteStatus, expectedReadyStatus metav1.ConditionStatus,
	) {
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.GatewayConfig, types.NamespacedName{Name: gatewayConfigName}),
			WithCondition(And(
				jq.Match(".status.additionalIngresses | length == %d", expectedCount),
				jq.Match(`.metadata.generation as $generation | any(.status.conditions[];
					.type == "AdditionalGatewaysReady" and .status == "True" and .observedGeneration == $generation)`),
				jq.Match(".status.additionalIngresses[] | select(.name == \"%s\" and .hostname == \"%s\") | .conditions | length == 4", name, hostname),
				jq.Match(`.status.additionalIngresses[] | select(.name == "%s") |
					.gatewayRef == {name: "%s", namespace: "%s"}`,
					name, name, tc.gatewayNamespace()),
				jq.Match(`.status.additionalIngresses[] |
					select(.name == "%s") |
					[.conditions[].type] | sort == ["AuthenticationReady", "GatewayReady", "Ready", "RouteAdmitted"]`, name),
				jq.Match(".status.additionalIngresses[] | select(.name == \"%s\") | any(.conditions[]; .type == \"GatewayReady\" and .status == \"True\")", name),
				jq.Match(".status.additionalIngresses[] | select(.name == \"%s\") | any(.conditions[]; .type == \"RouteAdmitted\" and .status == \"%s\")", name, expectedRouteStatus),
				jq.Match(`.status.additionalIngresses[] |
					select(.name == "%s") |
					any(.conditions[]; .type == "AuthenticationReady" and .status == "Unknown" and .reason == "StatusUnavailable")`, name),
				jq.Match(".status.additionalIngresses[] | select(.name == \"%s\") | any(.conditions[]; .type == \"Ready\" and .status == \"%s\")", name, expectedReadyStatus),
				jq.Match(`(.metadata.generation as $generation |
					.status.additionalIngresses[] | select(.name == "%s") |
					all(.conditions[]; .observedGeneration == $generation and .reason != "" and .message != ""))`, name),
			)),
			WithCustomErrorMsg("GatewayConfig should report status for additional ingress %s", name),
		)
	}

	spec := original.Spec
	spec.AdditionalIngresses = serviceApi.AdditionalIngresses{alpha}
	update(spec.AdditionalIngresses)
	tc.validateAdditionalGatewayAndRoute(t, alpha)
	validateStatus(1, alpha.Name, alpha.Hostname, metav1.ConditionTrue, metav1.ConditionTrue)
	checkDefault()

	g.Expect(authHTTPRoute.Spec.ParentRefs).NotTo(BeEmpty())
	g.Expect(authHTTPRoute.Spec.ParentRefs[0].Name).To(Equal(gwapiv1.ObjectName(gateway.GetDefaultGatewayName())))
	g.Expect(authHTTPRoute.Spec.ParentRefs[0].SectionName).To(BeNil())

	spec.AdditionalIngresses = serviceApi.AdditionalIngresses{alpha, beta}
	update(spec.AdditionalIngresses)
	tc.validateAdditionalGatewayAndRoute(t, alpha)
	tc.validateAdditionalGatewayAndRoute(t, beta)
	validateStatus(2, alpha.Name, alpha.Hostname, metav1.ConditionTrue, metav1.ConditionTrue)
	validateStatus(2, beta.Name, beta.Hostname, metav1.ConditionFalse, metav1.ConditionFalse)
	checkDefault()
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.GatewayConfig, types.NamespacedName{Name: gatewayConfigName}),
		WithCondition(jq.Match(".status.conditions[] | select(.type == \"Ready\") | .status == \"True\"")),
		WithCustomErrorMsg("a missing additional IngressController must not block GatewayConfig readiness when all Gateways are accepted"),
	)

	alphaRouteKey := types.NamespacedName{
		Name: alpha.Name, Namespace: tc.gatewayNamespace(),
	}
	validateAdditionalIngressRouteAdmissionStatus(t, tc, alphaRouteKey, alpha.Name, alpha.Hostname, ingressControllerName)
	currentConfig := &serviceApi.GatewayConfig{}
	g.Expect(tc.Client().Get(ctx, types.NamespacedName{Name: gatewayConfigName}, currentConfig)).To(Succeed())
	validateAdditionalIngressRouteSelectorChange(t, tc, ingressControllerKey, alphaRouteKey,
		alpha.Hostname, currentConfig.Generation, validateStatus)

	defaultGateway, err := tc.getGateway(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(defaultGateway.UID).To(Equal(originalGateway.UID))
	g.Expect(defaultGateway.Spec.Listeners).To(Equal(originalGateway.Spec.Listeners))
	g.Expect(authHTTPRoute.Spec.ParentRefs[0].SectionName).To(BeNil())

	spec.AdditionalIngresses = serviceApi.AdditionalIngresses{beta}
	update(spec.AdditionalIngresses)
	validateStatus(1, beta.Name, beta.Hostname, metav1.ConditionFalse, metav1.ConditionFalse)
	g.Eventually(func() bool {
		return k8serr.IsNotFound(tc.Client().Get(ctx, types.NamespacedName{
			Name: alpha.Name, Namespace: tc.gatewayNamespace(),
		}, &gwapiv1.Gateway{}))
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(BeTrue())
	g.Eventually(func() bool {
		return k8serr.IsNotFound(tc.Client().Get(ctx, alphaRouteKey, &routev1.Route{}))
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(BeTrue())
	checkDefault()

	spec.AdditionalIngresses = nil
	update(spec.AdditionalIngresses)
	g.Eventually(func() bool {
		return k8serr.IsNotFound(tc.Client().Get(ctx, types.NamespacedName{
			Name: beta.Name, Namespace: tc.gatewayNamespace(),
		}, &gwapiv1.Gateway{}))
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(BeTrue())
	g.Eventually(func() bool {
		return k8serr.IsNotFound(tc.Client().Get(ctx, types.NamespacedName{
			Name: beta.Name, Namespace: tc.gatewayNamespace(),
		}, &routev1.Route{}))
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(BeTrue())
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.GatewayConfig, types.NamespacedName{Name: gatewayConfigName}),
		WithCondition(And(
			jq.Match(".status.additionalIngresses | length == 0"),
			jq.Match(`any(.status.conditions[]; .type == "AdditionalGatewaysReady" and .status == "True" and .reason == "NoAdditionalGateways")`),
			jq.Match(`any(.status.conditions[]; .type == "Ready" and .status == "True")`),
		)),
		WithCustomErrorMsg("GatewayConfig should remove additional ingress status entries"),
	)
	checkDefault()
}

func (tc *GatewayTestCtx) validateAdditionalGatewayAndRoute(
	t *testing.T,
	ingress serviceApi.AdditionalIngress,
) {
	t.Helper()
	g := NewWithT(t)
	ctx := tc.Context()
	g.Eventually(func() error {
		additionalGateway := &gwapiv1.Gateway{}
		if err := tc.Client().Get(ctx, types.NamespacedName{
			Name: ingress.Name, Namespace: tc.gatewayNamespace(),
		}, additionalGateway); err != nil {
			return err
		}
		if additionalGateway.Spec.GatewayClassName != gwapiv1.ObjectName(gateway.GatewayClassName) {
			return fmt.Errorf("additional Gateway has unexpected GatewayClass %q", additionalGateway.Spec.GatewayClassName)
		}
		if len(additionalGateway.Spec.Listeners) != 1 {
			return fmt.Errorf("additional Gateway has %d listeners", len(additionalGateway.Spec.Listeners))
		}
		listener := additionalGateway.Spec.Listeners[0]
		if listener.Name != gateway.DefaultGatewayListenerName ||
			listener.Port != gwapiv1.PortNumber(gateway.StandardHTTPSPort) ||
			listener.Protocol != gwapiv1.HTTPSProtocolType {
			return fmt.Errorf("additional Gateway has unexpected listener %+v", listener)
		}
		secretName := additionalGateway.Name + "-service-tls"
		if listener.TLS == nil || len(listener.TLS.CertificateRefs) != 1 ||
			listener.TLS.CertificateRefs[0].Name != gwapiv1.ObjectName(secretName) ||
			listener.AllowedRoutes == nil {
			return errors.New("additional Gateway has no TLS certificate or allowed routes")
		}
		if additionalGateway.Spec.Infrastructure == nil || additionalGateway.Spec.Infrastructure.ParametersRef == nil {
			return errors.New("additional Gateway has no infrastructure ConfigMap reference")
		}
		configMap := &corev1.ConfigMap{}
		if err := tc.Client().Get(ctx, types.NamespacedName{
			Name: additionalGateway.Spec.Infrastructure.ParametersRef.Name, Namespace: tc.gatewayNamespace(),
		}, configMap); err != nil {
			return err
		}
		if !strings.Contains(configMap.Data["service"], secretName) {
			return errors.New("additional Gateway Service is not configured for a serving certificate")
		}
		if err := tc.Client().Get(ctx, types.NamespacedName{
			Name: secretName, Namespace: tc.gatewayNamespace(),
		}, &corev1.Secret{}); err != nil {
			return err
		}

		service := &corev1.Service{}
		if err := tc.Client().Get(ctx, types.NamespacedName{
			Name: gateway.GetGatewayServiceFullName(ingress.Name), Namespace: tc.gatewayNamespace(),
		}, service); err != nil {
			return err
		}
		var endpointPort intstr.IntOrString
		for _, port := range service.Spec.Ports {
			if port.Port == gateway.StandardHTTPSPort && (port.Protocol == "" || port.Protocol == corev1.ProtocolTCP) {
				endpointPort = port.TargetPort
				if endpointPort == (intstr.IntOrString{}) {
					endpointPort = intstr.FromInt32(port.Port)
				}
				break
			}
		}
		if endpointPort == (intstr.IntOrString{}) {
			return errors.New("additional Gateway Service has no HTTPS endpoint port")
		}
		route := &routev1.Route{}
		err := tc.Client().Get(ctx, types.NamespacedName{
			Name: ingress.Name, Namespace: tc.gatewayNamespace(),
		}, route)
		if err != nil {
			return err
		}
		return validateAdditionalBridgeRoute(route, ingress, service.Name, endpointPort)
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed())
}

func validateAdditionalBridgeRoute(
	route *routev1.Route,
	ingress serviceApi.AdditionalIngress,
	serviceName string,
	endpointPort intstr.IntOrString,
) error {
	if route.Spec.Host != ingress.Hostname || route.Spec.To.Name != serviceName {
		return fmt.Errorf("bridge Route targets Service %q and host %q", route.Spec.To.Name, route.Spec.Host)
	}
	if route.Spec.Port == nil {
		return errors.New("bridge Route has no target port")
	}
	if route.Spec.Port.TargetPort != endpointPort {
		return fmt.Errorf("bridge Route targets %s, Service endpoint uses %s", route.Spec.Port.TargetPort.String(), endpointPort.String())
	}
	if route.Spec.TLS == nil || route.Spec.TLS.Termination != routev1.TLSTerminationReencrypt ||
		route.Annotations["router.openshift.io/service-ca-certificate"] != "true" {
		return errors.New("bridge Route has no reencrypt TLS or service CA annotation")
	}
	for key, value := range ingress.RouteLabels {
		if route.Labels[key] != value {
			return fmt.Errorf("bridge Route label %q is %q, expected %q", key, route.Labels[key], value)
		}
	}
	return nil
}

func (tc *GatewayTestCtx) assertDefaultGatewayContinuity(
	ctx context.Context, t *testing.T, gatewayUID, routeUID types.UID, hostname string,
) {
	t.Helper()
	g := NewWithT(t)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.GatewayConfig, types.NamespacedName{Name: gatewayConfigName}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`)),
	)
	currentGateway, err := tc.getGateway(ctx)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(currentGateway.UID).To(Equal(gatewayUID))
	currentRoute := &routev1.Route{}
	g.Expect(tc.Client().Get(ctx, types.NamespacedName{
		Name: tc.gatewayName(), Namespace: tc.gatewayNamespace(),
	}, currentRoute)).To(Succeed())
	g.Expect(currentRoute.UID).To(Equal(routeUID))
	httpClient := tc.createHTTPClient()
	g.Eventually(func() error {
		requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, "https://"+hostname, nil)
		if err != nil {
			return err
		}
		response, err := httpClient.Do(request)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusFound && response.StatusCode != http.StatusTemporaryRedirect {
			return fmt.Errorf("default hostname returned %d, expected authentication redirect", response.StatusCode)
		}
		if response.Header.Get("Location") == "" {
			return errors.New("default hostname redirect has no Location header")
		}
		return nil
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed())
}

func (tc *GatewayTestCtx) logAdditionalIngressEvidence(t *testing.T, ingressControllerKey types.NamespacedName) {
	t.Helper()
	if !t.Failed() {
		return
	}
	ctx := tc.Context()
	logObject := func(name string, object any) {
		data, err := json.MarshalIndent(object, "", "  ")
		if err != nil {
			t.Logf("%s: %v", name, err)
			return
		}
		t.Logf("%s:\n%s", name, data)
	}
	currentConfig := &serviceApi.GatewayConfig{}
	if err := tc.Client().Get(ctx, types.NamespacedName{Name: gatewayConfigName}, currentConfig); err == nil {
		logObject("GatewayConfig", currentConfig)
	}
	if current, err := tc.getGateway(ctx); err == nil {
		logObject("Gateway", current)
	}
	services := &corev1.ServiceList{}
	if err := tc.Client().List(ctx, services, client.InNamespace(tc.gatewayNamespace()),
		client.MatchingLabels{labels.GatewayAPI.GatewayName: tc.gatewayName()}); err == nil {
		logObject("Gateway Services", services.Items)
	}
	routes := &routev1.RouteList{}
	if err := tc.Client().List(ctx, routes, client.InNamespace(tc.gatewayNamespace())); err == nil {
		for _, route := range routes.Items {
			logObject("Route "+route.Name, route)
		}
	}
	controller := &operatorv1.IngressController{}
	if err := tc.Client().Get(ctx, ingressControllerKey, controller); err == nil {
		logObject("IngressController", controller)
	}
}

func validateAdditionalIngressRouteAdmissionStatus(
	t *testing.T,
	tc *GatewayTestCtx,
	routeKey types.NamespacedName,
	ingressName, hostname, targetRouter string,
) {
	t.Helper()
	g := NewWithT(t)
	ctx := tc.Context()
	g.Eventually(func() error {
		route := &routev1.Route{}
		if err := tc.Client().Get(ctx, routeKey, route); err != nil {
			return err
		}
		admitted := make(map[string]struct{})
		for _, router := range route.Status.Ingress {
			if router.Host != hostname {
				continue
			}
			for _, condition := range router.Conditions {
				if condition.Type == routev1.RouteAdmitted && condition.Status == corev1.ConditionTrue && router.RouterName != "" {
					admitted[router.RouterName] = struct{}{}
				}
			}
		}
		if _, found := admitted[targetRouter]; !found {
			return fmt.Errorf("target IngressController %q has not admitted Route %q", targetRouter, routeKey.Name)
		}
		reason := "Ready"
		severity := ""
		if len(admitted) > 1 {
			reason = "MultipleIngressControllers"
			severity = "Info"
		}
		current := &serviceApi.GatewayConfig{}
		if err := tc.Client().Get(ctx, types.NamespacedName{Name: gatewayConfigName}, current); err != nil {
			return err
		}
		for _, status := range current.Status.AdditionalIngresses {
			if status.Name != ingressName {
				continue
			}
			for _, condition := range status.Conditions {
				if condition.Type == serviceApi.AdditionalIngressRouteAdmittedConditionType &&
					condition.Status == metav1.ConditionTrue && condition.Reason == reason &&
					string(condition.Severity) == severity {
					return nil
				}
			}
		}
		return fmt.Errorf("RouteAdmitted condition does not reflect %d admitting IngressControllers", len(admitted))
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed())
}

func restoreAdditionalIngressGatewayTest(
	t *testing.T,
	tc *GatewayTestCtx,
	original *serviceApi.GatewayConfig,
	originalGateway *gwapiv1.Gateway,
	ingressControllerKey types.NamespacedName,
) {
	t.Helper()
	g := NewWithT(t)
	ctx := tc.Context()
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &serviceApi.GatewayConfig{}
		if err := tc.Client().Get(ctx, types.NamespacedName{Name: gatewayConfigName}, current); err != nil {
			return err
		}
		current.Spec.AdditionalIngresses = original.Spec.AdditionalIngresses
		return tc.Client().Update(ctx, current)
	})
	if err != nil {
		t.Errorf("failed to restore GatewayConfig: %v", err)
	} else {
		g.Eventually(func() bool {
			currentConfig := &serviceApi.GatewayConfig{}
			if err := tc.Client().Get(ctx, types.NamespacedName{Name: gatewayConfigName}, currentConfig); err != nil {
				return false
			}
			currentGateway, err := tc.getGateway(ctx)
			if err != nil {
				return false
			}
			return reflect.DeepEqual(currentConfig.Spec.AdditionalIngresses, original.Spec.AdditionalIngresses) &&
				reflect.DeepEqual(additionalIngressStatusNames(currentConfig.Status.AdditionalIngresses), additionalIngressStatusNames(original.Status.AdditionalIngresses)) &&
				reflect.DeepEqual(currentGateway.Spec.Listeners, originalGateway.Spec.Listeners)
		}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(BeTrue())
	}
	if err := tc.Client().Delete(ctx, &operatorv1.IngressController{ObjectMeta: metav1.ObjectMeta{
		Name: ingressControllerKey.Name, Namespace: ingressControllerKey.Namespace,
	}}); err != nil && !k8serr.IsNotFound(err) {
		t.Errorf("failed to delete test IngressController: %v", err)
		return
	}
	g.Eventually(func() bool {
		current := &operatorv1.IngressController{}
		return k8serr.IsNotFound(tc.Client().Get(ctx, ingressControllerKey, current))
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(BeTrue())
}

func additionalIngressStatusNames(entries []serviceApi.AdditionalIngressStatus) map[string]struct{} {
	names := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		names[entry.Name] = struct{}{}
	}
	return names
}

func validateAdditionalIngressRouteSelectorChange(
	t *testing.T,
	tc *GatewayTestCtx,
	ingressControllerKey types.NamespacedName,
	routeKey types.NamespacedName,
	expectedHostname string,
	expectedGeneration int64,
	validateStatus func(int, string, string, metav1.ConditionStatus, metav1.ConditionStatus),
) {
	t.Helper()
	g := NewWithT(t)
	ctx := tc.Context()
	g.Eventually(func() error {
		route := &routev1.Route{}
		if err := tc.Client().Get(ctx, routeKey, route); err != nil {
			return err
		}
		if route.Spec.Host != expectedHostname {
			return fmt.Errorf("additional Route host is %q, expected %q", route.Spec.Host, expectedHostname)
		}
		return nil
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed())
	g.Expect(retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &operatorv1.IngressController{}
		if err := tc.Client().Get(ctx, ingressControllerKey, current); err != nil {
			return err
		}
		current.Spec.RouteSelector = &metav1.LabelSelector{MatchLabels: map[string]string{
			"example.com/ingress": "no-longer-e2e-alpha",
		}}
		return tc.Client().Update(ctx, current)
	})).To(Succeed())
	g.Eventually(func() error {
		current := &serviceApi.GatewayConfig{}
		if err := tc.Client().Get(ctx, types.NamespacedName{Name: gatewayConfigName}, current); err != nil {
			return err
		}
		if current.Generation != expectedGeneration {
			return fmt.Errorf("GatewayConfig generation changed from %d to %d", expectedGeneration, current.Generation)
		}
		route := &routev1.Route{}
		if err := tc.Client().Get(ctx, routeKey, route); err != nil {
			return err
		}
		for _, router := range route.Status.Ingress {
			if router.Host != expectedHostname || router.RouterName != ingressControllerKey.Name {
				continue
			}
			for _, condition := range router.Conditions {
				if condition.Type == routev1.RouteAdmitted && condition.Status == corev1.ConditionTrue {
					return fmt.Errorf("IngressController %q still admits Route %q", ingressControllerKey.Name, routeKey.Name)
				}
			}
		}
		return nil
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed())
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.GatewayConfig, types.NamespacedName{Name: gatewayConfigName}),
		WithCondition(jq.Match(
			`.status.additionalIngresses[] | select(.name == "e2e-alpha") | `+
				`any(.conditions[]; .type == "RouteAdmitted" and .status != "True")`)),
		WithCustomErrorMsg("GatewayConfig should report that the target IngressController stopped admitting the retained Route"),
	)
	validateStatus(2, "e2e-beta", "e2e-beta.example.com", metav1.ConditionFalse, metav1.ConditionFalse)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.GatewayConfig, types.NamespacedName{Name: gatewayConfigName}),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`)),
		WithCustomErrorMsg("additional Route admission failures must not block readiness when all Gateways are accepted"),
	)
	defaultRoute := &routev1.Route{}
	g.Expect(tc.Client().Get(ctx, types.NamespacedName{
		Name: tc.gatewayName(), Namespace: tc.gatewayNamespace(),
	}, defaultRoute)).To(Succeed())
	g.Expect(retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &operatorv1.IngressController{}
		if err := tc.Client().Get(ctx, ingressControllerKey, current); err != nil {
			return err
		}
		current.Spec.RouteSelector = &metav1.LabelSelector{MatchLabels: map[string]string{
			"example.com/ingress": "e2e-alpha",
		}}
		return tc.Client().Update(ctx, current)
	})).To(Succeed())
	g.Eventually(func() error {
		route := &routev1.Route{}
		if err := tc.Client().Get(ctx, routeKey, route); err != nil {
			return err
		}
		if route.Spec.Host != expectedHostname {
			return fmt.Errorf("additional Route host is %q, expected %q", route.Spec.Host, expectedHostname)
		}
		return nil
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed())
	validateStatus(2, "e2e-alpha", expectedHostname, metav1.ConditionTrue, metav1.ConditionTrue)
	validateStatus(2, "e2e-beta", "e2e-beta.example.com", metav1.ConditionFalse, metav1.ConditionFalse)
}

// ValidateXKSCertManagerCertificates verifies that XKS gateway TLS certificates are issued by
// cert-manager rather than generated directly by the gateway controller. It checks both the
// Gateway listener certificate and the kube-auth-proxy certificate, including the resulting TLS
// Secrets populated by cert-manager.
func (tc *GatewayTestCtx) ValidateXKSCertManagerCertificates(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)
	if !tc.IsXKS() {
		t.Skip("Skipping test because cert-manager gateway certificates are XKS-only")
	}

	issuerName, issuerKind := tc.getXKSCertManagerIssuer(t)
	gatewayHostname := tc.getExpectedGatewayHostname(t)
	gatewayNamespace := tc.gatewayNamespace()

	certificates := []struct {
		name       string
		secretName string
		dnsName    string
	}{
		{
			name:       tc.getTLSSecretName(t),
			secretName: tc.getTLSSecretName(t),
			dnsName:    gatewayHostname,
		},
		{
			name:       kubeAuthProxyTLSName,
			secretName: kubeAuthProxyTLSName,
			dnsName:    fmt.Sprintf("%s.%s.svc.cluster.local", kubeAuthProxyName, gatewayNamespace),
		},
	}

	for _, certificate := range certificates {
		t.Run(certificate.name, func(t *testing.T) {
			t.Helper()
			t.Logf("Validating cert-manager Certificate %s/%s", gatewayNamespace, certificate.name)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.CertManagerCertificate, types.NamespacedName{
					Name:      certificate.name,
					Namespace: gatewayNamespace,
				}),
				WithCondition(And(
					jq.Match(`.spec.secretName == "%s"`, certificate.secretName),
					jq.Match(`.spec.dnsNames == ["%s"]`, certificate.dnsName),
					jq.Match(`.spec.issuerRef.name == "%s"`, issuerName),
					jq.Match(`.spec.issuerRef.kind == "%s"`, issuerKind),
					jq.Match(`.spec.issuerRef.group == "%s"`, gvk.CertManagerCertificate.Group),
					jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`),
				)),
				WithEventuallyTimeout(tc.TestTimeouts.authGatewayTimeout),
				WithCustomErrorMsg("cert-manager Certificate should be issued with the expected issuer and SAN"),
			)

			tc.EnsureResourceExists(
				WithMinimalObject(gvk.Secret, types.NamespacedName{
					Name:      certificate.secretName,
					Namespace: gatewayNamespace,
				}),
				WithCondition(And(
					jq.Match(`.type == "%s"`, string(corev1.SecretTypeTLS)),
					jq.Match(`.data."tls.crt" | length > 0`),
					jq.Match(`.data."tls.key" | length > 0`),
				)),
				WithEventuallyTimeout(tc.TestTimeouts.authGatewayTimeout),
				WithCustomErrorMsg("cert-manager should populate a non-empty TLS Secret"),
			)
		})
	}
}

// ValidateXKSCertificateReadinessRecovery verifies that failed cert-manager issuance
// makes GatewayConfig unready and that restoring the issuer recovers readiness.
func (tc *GatewayTestCtx) ValidateXKSCertificateReadinessRecovery(t *testing.T) {
	t.Helper()
	skipUnless(t, Tier1)
	if !tc.IsXKS() {
		t.Skip("Skipping test because cert-manager gateway certificates are XKS-only")
	}

	ctx := tc.Context()
	configKey := types.NamespacedName{Name: gatewayConfigName}
	gatewayConfig := &serviceApi.GatewayConfig{}
	require.NoError(t, tc.Client().Get(ctx, configKey, gatewayConfig))
	require.NotNil(t, gatewayConfig.Spec.Certificate)
	require.Equal(t, infrav1.SelfSigned, gatewayConfig.Spec.Certificate.Type)
	require.NotNil(t, gatewayConfig.Spec.OIDC)
	originalCertificate := gatewayConfig.DeepCopy().Spec.Certificate

	setCertificate := func(secretName string, ref *infrav1.IssuerRef) error {
		return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			current := &serviceApi.GatewayConfig{}
			if err := tc.Client().Get(ctx, configKey, current); err != nil {
				return err
			}
			if current.Spec.Certificate == nil {
				return errors.New("GatewayConfig certificate configuration disappeared")
			}
			if current.Spec.Certificate.SecretName == secretName && reflect.DeepEqual(current.Spec.Certificate.IssuerRef, ref) {
				return nil
			}
			current.Spec.Certificate.SecretName = secretName
			current.Spec.Certificate.IssuerRef = ref
			return tc.Client().Update(ctx, current)
		})
	}
	t.Cleanup(func() {
		if err := setCertificate(originalCertificate.SecretName, originalCertificate.IssuerRef); err != nil {
			t.Errorf("failed to restore GatewayConfig certificate configuration: %v", err)
		}
	})

	uniqueID := time.Now().UnixNano()
	missingIssuerName := fmt.Sprintf("e2e-missing-issuer-%d", uniqueID)
	newSecretName := fmt.Sprintf("e2e-unissued-gateway-tls-%d", uniqueID)
	require.NoError(t, setCertificate(newSecretName, &infrav1.IssuerRef{Name: missingIssuerName, Kind: "ClusterIssuer"}))

	newCertKey := types.NamespacedName{Name: newSecretName, Namespace: tc.gatewayNamespace()}
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.CertManagerCertificate, newCertKey),
		WithCondition(And(
			jq.Match(`.spec.issuerRef.name == "%s"`, missingIssuerName),
			jq.Match(`.spec.secretName == "%s"`, newSecretName),
		)),
		WithEventuallyTimeout(tc.TestTimeouts.authGatewayTimeout),
		WithCustomErrorMsg("Gateway Certificate should request a new Secret from the missing issuer"),
	)

	// Observe the missing Secret and GatewayConfig failure in the same poll. The
	// unique name prevents a prior issuance from satisfying the Secret check.
	g := NewWithT(t)
	g.Eventually(func(g Gomega) {
		secret := &corev1.Secret{}
		g.Expect(k8serr.IsNotFound(tc.Client().Get(ctx, newCertKey, secret))).To(BeTrue())

		current := &serviceApi.GatewayConfig{}
		g.Expect(tc.Client().Get(ctx, configKey, current)).To(Succeed())
		g.Expect(current.Status.Conditions).To(ContainElement(And(
			HaveField("Type", gateway.ReadyConditionType),
			HaveField("Status", metav1.ConditionFalse),
			HaveField("Message", MatchRegexp("Certificate|TLS Secret")),
		)))
		g.Expect(current.Status.Conditions).To(ContainElement(And(
			HaveField("Type", "Ready"),
			HaveField("Status", metav1.ConditionFalse),
		)))
	}, tc.TestTimeouts.authGatewayTimeout, 2*time.Second).Should(Succeed())

	require.NoError(t, setCertificate(originalCertificate.SecretName, originalCertificate.IssuerRef))
	tc.ValidateXKSCertManagerCertificates(t)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.GatewayConfig, configKey),
		WithCondition(And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "True"`, gateway.ReadyConditionType),
			jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`),
		)),
		WithEventuallyTimeout(tc.TestTimeouts.authGatewayTimeout),
		WithCustomErrorMsg("GatewayConfig should recover after restoring the issuer"),
	)
}

// getXKSCertManagerIssuer reads the issuer configuration injected into the operator deployment.
// This keeps the e2e assertion valid for both ODH and RHOAI platform defaults without hardcoding
// a particular CA issuer name into the test.
func (tc *GatewayTestCtx) getXKSCertManagerIssuer(t *testing.T) (string, string) {
	t.Helper()

	operatorDeployment := &appsv1.Deployment{}
	tc.FetchTypedResource(
		operatorDeployment,
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      tc.getControllerDeploymentName(),
			Namespace: tc.OperatorNamespace,
		}),
	)

	issuerName := "opendatahub-ca-issuer"
	issuerKind := certmanager.DefaultIssuerRefKind
	for _, container := range operatorDeployment.Spec.Template.Spec.Containers {
		for _, envVar := range container.Env {
			switch envVar.Name {
			case certmanager.EnvCAIssuerName:
				if envVar.Value != "" {
					issuerName = envVar.Value
				}
			case certmanager.EnvIssuerRefKind:
				if envVar.Value != "" {
					issuerKind = envVar.Value
				}
			}
		}
	}

	t.Logf("Expected XKS cert-manager issuer: %s/%s", issuerKind, issuerName)
	return issuerName, issuerKind
}

// ValidateOAuthClientAndSecret validates OpenShift OAuth client and proxy secret creation.
func (tc *GatewayTestCtx) ValidateOAuthClientAndSecret(t *testing.T) {
	t.Helper()

	tc.SkipIfXKSCluster(t)
	skipUnless(t, Tier1)
	tc.SkipIfBYOIDC(t)
	t.Log("Validating OAuth client and secret creation")

	expectedGatewayHostname := tc.getExpectedGatewayHostname(t)
	expectedRedirectURI := "https://" + expectedGatewayHostname + gateway.OAuthCallbackPath

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
			Namespace: tc.gatewayNamespace(),
		}),
		WithCondition(And(
			jq.Match(`.type == "%s"`, string(corev1.SecretTypeOpaque)),
			jq.Match(`.metadata.labels["%s"] == "%s"`, labels.PlatformPartOf, gateway.PartOfGatewayConfig),
			jq.Match(`.data | has("OAUTH2_PROXY_CLIENT_ID")`),
			jq.Match(`.data | has("OAUTH2_PROXY_CLIENT_SECRET")`),
			jq.Match(`.data | has("OAUTH2_PROXY_COOKIE_SECRET")`),
			jq.Match(`.data.OAUTH2_PROXY_CLIENT_SECRET | length > 0`),
			jq.Match(`.data.OAUTH2_PROXY_COOKIE_SECRET | length > 0`),
		)),
		WithCustomErrorMsg("OAuth proxy credentials secret should be Opaque type with %s=%s label, "+
			"exactly %d non-empty keys, and CLIENT_ID matching OAuthClient name", labels.PlatformPartOf, gateway.PartOfGatewayConfig, expectedSecretDataKeys),
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

	tc.SkipIfXKSCluster(t)
	skipUnless(t, Tier1)
	tc.SkipIfBYOIDC(t)
	t.Log("Validating kube-auth-proxy deployment and service")

	expectedGatewayHostname := tc.getExpectedGatewayHostname(t)
	expectedRedirectURL := makeRedirectURL(expectedGatewayHostname)
	expectedCookieDomain := makeCookieDomain(expectedGatewayHostname)
	tlsMinVersionArg, tlsCipherSuitesArg, tlsCurvePreferencesArg := tc.expectedKubeAuthProxyTLSDeploymentArgs(t)

	// kube-auth-proxy deployment checks (many conditions grouped into a single EnsureResourceExists call)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      kubeAuthProxyName,
			Namespace: tc.gatewayNamespace(),
		}),
		WithCondition(And(
			// replica count (minimum 2 for HPA)
			jq.Match(`.spec.replicas == 2`),

			// basic pod template checks
			jq.Match(`.spec.selector.matchLabels.app == "%s"`, kubeAuthProxyName),
			jq.Match(`.spec.template.spec.containers | length > 0`),
			jq.Match(`.spec.template.spec.containers[0].name == "%s"`, kubeAuthProxyName),

			// pod security context checks
			jq.Match(`.spec.template.spec.securityContext.runAsNonRoot == true`),
			jq.Match(`.spec.template.spec.securityContext.seccompProfile.type == "RuntimeDefault"`),

			// container security context checks
			jq.Match(`.spec.template.spec.containers[0].securityContext.readOnlyRootFilesystem == true`),
			jq.Match(`.spec.template.spec.containers[0].securityContext.allowPrivilegeEscalation == false`),
			jq.Match(`.spec.template.spec.containers[0].securityContext.capabilities.drop | length > 0`),
			jq.Match(`.spec.template.spec.containers[0].securityContext.capabilities.drop[] | . == "ALL"`),

			// ports
			jq.Match(`.spec.template.spec.containers[0].ports | length == 3`),
			jq.Match(`.spec.template.spec.containers[0].ports[] | select(.name == "http") | .containerPort == %d`, kubeAuthProxyHTTPPort),
			jq.Match(`.spec.template.spec.containers[0].ports[] | select(.name == "https") | .containerPort == %d`, kubeAuthProxyHTTPSPort),
			jq.Match(`.spec.template.spec.containers[0].ports[] | select(.name == "metrics") | .containerPort == %d`, kubeAuthProxyMetricsPort),

			// env from secret
			jq.Match(`.spec.template.spec.containers[0].env | length == 3`),
			jq.Match(`.spec.template.spec.containers[0].env[] | select(.name == "%s") | .valueFrom.secretKeyRef.name == "%s"`, gateway.EnvClientID, kubeAuthProxyCredsName),
			jq.Match(`.spec.template.spec.containers[0].env[] | select(.name == "%s") | .valueFrom.secretKeyRef.name == "%s"`, gateway.EnvClientSecret, kubeAuthProxyCredsName),
			jq.Match(`.spec.template.spec.containers[0].env[] | select(.name == "%s") | .valueFrom.secretKeyRef.name == "%s"`, gateway.EnvCookieSecret, kubeAuthProxyCredsName),

			// TLS volume mount
			jq.Match(`.spec.template.spec.containers[0].volumeMounts[] | select(.name == "tls-certs") | .mountPath == "/etc/tls/private"`),
			jq.Match(`.spec.template.spec.containers[0].volumeMounts[] | select(.name == "tls-certs") | .readOnly == true`),
			jq.Match(`.spec.template.spec.volumes[] | select(.name == "tls-certs") | .secret.secretName == "%s"`, kubeAuthProxyTLSName),

			// /tmp volume mount (required for read-only root filesystem)
			jq.Match(`.spec.template.spec.containers[0].volumeMounts[] | select(.name == "tmp") | .mountPath == "/tmp"`),
			jq.Match(`.spec.template.spec.volumes[] | select(.name == "tmp") | .emptyDir.medium == "Memory"`),
			jq.Match(`.spec.template.spec.volumes[] | select(.name == "tmp") | .emptyDir.sizeLimit == "10Mi"`),

			// critical args and behavior
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--provider=openshift")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--scope=user:full")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "%s")`, expectedRedirectURL),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "%s")`, expectedCookieDomain),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--https-address=0.0.0.0:%d")`, kubeAuthProxyHTTPSPort),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--http-address=0.0.0.0:%d")`, kubeAuthProxyHTTPPort),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--tls-cert-file=/etc/tls/private/tls.crt")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--tls-key-file=/etc/tls/private/tls.key")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "%s")`, tlsMinVersionArg),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "%s")`, tlsCipherSuitesArg),
			kubeAuthProxyCurvePreferencesMatcher(tlsCurvePreferencesArg),

			// cookie config and related flags
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--cookie-secure=true")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--cookie-httponly=true")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--cookie-samesite=lax")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--cookie-name=_oauth2_proxy")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--cookie-expire=24h0m0s")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--cookie-refresh=1h0m0s")`),

			// auth proxy behavior flags
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--skip-provider-button")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--skip-jwt-bearer-tokens=true")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--pass-access-token=true")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--set-xauthrequest=true")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--email-domain=*")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--upstream=static://200")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--enable-k8s-token-validation=true")`),

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
	tc.EnsureDeploymentReady(types.NamespacedName{Name: kubeAuthProxyName, Namespace: tc.gatewayNamespace()}, 2)

	// kube-auth-proxy service
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Service, types.NamespacedName{
			Name:      kubeAuthProxyName,
			Namespace: tc.gatewayNamespace(),
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
			Namespace: tc.gatewayNamespace(),
		}),
		WithCustomErrorMsg("kube-auth-proxy TLS secret should exist"),
	)

	t.Log("kube-auth-proxy deployment and service validation completed")
}

// ValidateHPA checks HPA configuration and reconciliation of maximum replica updates.
func (tc *GatewayTestCtx) ValidateHPA(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)
	t.Log("Validating HorizontalPodAutoscaler for kube-auth-proxy")

	ctx := tc.Context()
	configKey := types.NamespacedName{Name: gatewayConfigName}
	config := &serviceApi.GatewayConfig{}
	require.NoError(t, tc.Client().Get(ctx, configKey, config))
	originalMaximum := config.Spec.AuthProxyMaxReplicas
	expectedMaximum := int32(10)
	if originalMaximum != nil {
		expectedMaximum = *originalMaximum
	}

	validate := func(maximum int32) types.UID {
		return tc.EnsureResourceExists(
			WithMinimalObject(gvk.HorizontalPodAutoscaler, types.NamespacedName{
				Name:      kubeAuthProxyName,
				Namespace: tc.gatewayNamespace(),
			}),
			WithCondition(And(
				// Target deployment reference
				jq.Match(`.spec.scaleTargetRef.apiVersion == "apps/v1"`),
				jq.Match(`.spec.scaleTargetRef.kind == "Deployment"`),
				jq.Match(`.spec.scaleTargetRef.name == "%s"`, kubeAuthProxyName),

				// Replica bounds
				jq.Match(`.spec.minReplicas == 2`),
				jq.Match(`.spec.maxReplicas == %d`, maximum),

				// Scale-down behavior: 5 min stabilization, 50% reduction per minute
				jq.Match(`.spec.behavior.scaleDown.stabilizationWindowSeconds == 300`),
				jq.Match(`.spec.behavior.scaleDown.policies[0].type == "Percent"`),
				jq.Match(`.spec.behavior.scaleDown.policies[0].value == 50`),
				jq.Match(`.spec.behavior.scaleDown.policies[0].periodSeconds == 60`),

				// Scale-up behavior: immediate, aggressive scaling
				jq.Match(`.spec.behavior.scaleUp.stabilizationWindowSeconds == 0`),
				jq.Match(`.spec.behavior.scaleUp.selectPolicy == "Max"`),

				// CPU utilization metric
				jq.Match(`.spec.metrics | length == 1`),
				jq.Match(`.spec.metrics[0].type == "Resource"`),
				jq.Match(`.spec.metrics[0].resource.name == "cpu"`),
				jq.Match(`.spec.metrics[0].resource.target.type == "Utilization"`),
				jq.Match(`.spec.metrics[0].resource.target.averageUtilization == 70`),
			)),
			WithEventuallyTimeout(tc.TestTimeouts.authGatewayTimeout),
			WithCustomErrorMsg("HPA should retain minimum 2 and reconcile maximum %d with CPU target=70%%", maximum),
		).GetUID()
	}

	originalUID := validate(expectedMaximum)
	setMaximum := func(maximum *int32) error {
		return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			current := &serviceApi.GatewayConfig{}
			if err := tc.Client().Get(ctx, configKey, current); err != nil {
				return err
			}
			current.Spec.AuthProxyMaxReplicas = maximum
			return tc.Client().Update(ctx, current)
		})
	}
	t.Cleanup(func() {
		if err := setMaximum(originalMaximum); err != nil {
			t.Errorf("failed to restore GatewayConfig authProxyMaxReplicas: %v", err)
			return
		}
		require.Equal(t, originalUID, validate(expectedMaximum), "restoration must update the existing HPA")
	})

	for _, maximum := range []int32{4, 2} {
		t.Logf("Updating default auth proxy maximum replicas to %d", maximum)
		require.NoError(t, setMaximum(&maximum))
		require.Equal(t, originalUID, validate(maximum), "scaling configuration must update the existing HPA")
	}

	t.Log("HorizontalPodAutoscaler validation completed")
}

// ValidateOAuthCallbackRoute validates the OAuth callback HTTPRoute configuration.
func (tc *GatewayTestCtx) ValidateOAuthCallbackRoute(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)
	t.Log("Validating OAuth callback HTTPRoute")

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.HTTPRoute, types.NamespacedName{
			Name:      oauthCallbackRouteName,
			Namespace: tc.gatewayNamespace(),
		}),
		WithCondition(And(
			// parent reference checks
			jq.Match(`.spec.parentRefs | length == 1`),
			jq.Match(`.spec.parentRefs[0].group == "%s"`, gwapiv1.GroupVersion.Group),
			jq.Match(`.spec.parentRefs[0].kind == "Gateway"`),
			jq.Match(`.spec.parentRefs[0].name == "%s"`, tc.gatewayName()),
			jq.Match(`.spec.parentRefs[0].namespace == "%s"`, tc.gatewayNamespace()),

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
			jq.Match(`.spec.rules[0].backendRefs[0].namespace == "%s"`, tc.gatewayNamespace()),
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

	skipUnless(t, Tier1)
	t.Log("Validating EnvoyFilter for authentication")

	authProxyFQDN := getServiceFQDN(kubeAuthProxyName, tc.gatewayNamespace())
	authProxyHostPort := net.JoinHostPort(authProxyFQDN, strconv.Itoa(kubeAuthProxyHTTPSPort))
	authProxyURI := "https://" + authProxyHostPort + "/oauth2/auth"
	// Istio auto-creates EDS clusters with this naming pattern for better load balancing
	istioEDSClusterName := fmt.Sprintf("outbound|%d||%s", kubeAuthProxyHTTPSPort, authProxyFQDN)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.EnvoyFilter, types.NamespacedName{
			Name:      envoyFilterName,
			Namespace: tc.gatewayNamespace(),
		}),
		WithCondition(And(
			jq.Match(`.metadata.labels["%s"] == "%s"`, labels.K8SCommon.PartOf, gateway.PartOfLabelValue),

			// workload selector
			jq.Match(`.spec.workloadSelector.labels."%s" == "%s"`, labels.GatewayAPI.GatewayName, tc.gatewayName()),

			jq.Match(`.spec.configPatches | length == 2`),

			// Patch 0: ext_authz
			jq.Match(`.spec.configPatches[0].applyTo == "HTTP_FILTER"`),
			jq.Match(`.spec.configPatches[0].match.context == "GATEWAY"`),
			jq.Match(`.spec.configPatches[0].patch.operation == "INSERT_BEFORE"`),
			jq.Match(`.spec.configPatches[0].patch.value.name == "envoy.filters.http.ext_authz"`),

			// ext_authz config - uses Istio's EDS cluster for better load balancing across all pods
			jq.Match(`.spec.configPatches[0].patch.value.typed_config.http_service.server_uri.cluster == "%s"`, istioEDSClusterName),
			jq.Match(`.spec.configPatches[0].patch.value.typed_config.http_service.server_uri.timeout == "5s"`),
			jq.Match(`.spec.configPatches[0].patch.value.typed_config.http_service.server_uri.uri == "%s"`, authProxyURI),

			// ext_authz allowed headers
			jq.Match(`.spec.configPatches[0].patch.value.typed_config.http_service.authorization_request.allowed_headers.patterns | any(.exact == "cookie")`),
			jq.Match(`.spec.configPatches[0].patch.value.typed_config.http_service.authorization_request.allowed_headers.patterns | any(.exact == "user-agent")`),
			jq.Match(`.spec.configPatches[0].patch.value.typed_config.http_service.authorization_response.allowed_client_headers.patterns[0].exact == "set-cookie"`),
			jq.Match(`.spec.configPatches[0].patch.value.typed_config.http_service.authorization_response.allowed_upstream_headers.patterns | any(.exact == "x-auth-request-user")`),
			jq.Match(`.spec.configPatches[0].patch.value.typed_config.http_service.authorization_response.allowed_upstream_headers.patterns | any(.exact == "x-auth-request-email")`),
			jq.Match(`.spec.configPatches[0].patch.value.typed_config.http_service.authorization_response.allowed_upstream_headers.patterns | any(.exact == "x-auth-request-access-token")`),
			jq.Match(`.spec.configPatches[0].patch.value.typed_config.http_service.authorization_response.allowed_upstream_headers.patterns | any(.exact == "authorization")`),

			// Patch 1: Lua filter token forwarding
			jq.Match(`.spec.configPatches[1].applyTo == "HTTP_FILTER"`),
			jq.Match(`.spec.configPatches[1].patch.value.name == "envoy.filters.http.lua"`),
			jq.Match(`.spec.configPatches[1].patch.value.typed_config.inline_code | contains("x-auth-request-access-token")`),
			jq.Match(`.spec.configPatches[1].patch.value.typed_config.inline_code | contains("x-auth-request-user")`),
			jq.Match(`.spec.configPatches[1].patch.value.typed_config.inline_code | contains("x-forwarded-access-token")`),
			jq.Match(`.spec.configPatches[1].patch.value.typed_config.inline_code | contains("Bearer")`),
			jq.Match(`.spec.configPatches[1].patch.value.typed_config.inline_code | contains("authorization")`),
		)),
		WithCustomErrorMsg("EnvoyFilter should be properly configured for authentication"),
	)

	t.Log("EnvoyFilter validation completed")
}

// ValidateEDSEndpointDiscovery validates that the Service is properly configured for EDS.
//
// This test verifies:
// - Kubernetes Service exists for kube-auth-proxy
// - Service has correct selector labels to match auth proxy pods
// - Service is properly configured for EDS to discover endpoints.
func (tc *GatewayTestCtx) ValidateEDSEndpointDiscovery(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)
	t.Log("Validating EDS service configuration for kube-auth-proxy")

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Service, types.NamespacedName{
			Name:      kubeAuthProxyName,
			Namespace: tc.gatewayNamespace(),
		}),
		WithCondition(And(
			jq.Match(`.spec.selector.app == "%s"`, kubeAuthProxyName),
			jq.Match(`.spec.ports[] | select(.name == "https") | .port == %d`, kubeAuthProxyHTTPSPort),
			jq.Match(`.spec.ports[] | select(.name == "https") | .targetPort == %d`, kubeAuthProxyHTTPSPort),
		)),
		WithCustomErrorMsg("kube-auth-proxy Service should exist with correct pod selector for EDS endpoint discovery"),
	)

	t.Log("EDS service configuration validation completed")
}

// ValidateGatewayReadyStatus validates Gateway resource is fully operational and ready to route traffic.
func (tc *GatewayTestCtx) ValidateGatewayReadyStatus(t *testing.T) {
	t.Helper()

	skipUnless(t, Smoke)
	t.Log("Validating Gateway ready status")

	if tc.IsXKS() {
		// On vanilla Kubernetes with LoadBalancer ingress mode, Istio won't set Programmed=True
		// when no cloud load balancer controller assigns an external IP (e.g. KinD).
		// Only check Accepted (config is valid) which Istio sets immediately.
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.KubernetesGateway, types.NamespacedName{
				Name:      tc.gatewayName(),
				Namespace: tc.gatewayNamespace(),
			}),
			WithCondition(
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, string(gwapiv1.GatewayConditionAccepted), string(metav1.ConditionTrue)),
			),
			WithCustomErrorMsg("Gateway should be accepted by the controller"),
		)
	} else {
		// On OpenShift with a real ingress controller, validate full readiness
		tc.EnsureResourceExists(
			WithMinimalObject(gvk.KubernetesGateway, types.NamespacedName{
				Name:      tc.gatewayName(),
				Namespace: tc.gatewayNamespace(),
			}),
			WithCondition(And(
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, string(gwapiv1.GatewayConditionAccepted), string(metav1.ConditionTrue)),
				jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, string(gwapiv1.GatewayConditionProgrammed), string(metav1.ConditionTrue)),
				jq.Match(`.status.listeners[] | select(.name == "%s") | .attachedRoutes >= 1`, defaultGatewayListenerName),
			)),
			WithCustomErrorMsg("Gateway should be fully operational with healthy listener"),
		)
	}

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

	tc.SkipIfXKSCluster(t)
	skipUnless(t, Tier1)
	tc.SkipIfBYOIDC(t)

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
	dashboardRouteName := getDashboardRouteNameByPlatform(tc.FetchPlatformRelease())

	t.Log("Waiting for dashboard HTTPRoute to be accepted by Gateway")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.HTTPRoute, types.NamespacedName{
			Name:      dashboardRouteName,
			Namespace: dashboardNamespace,
		}),
		WithCondition(And(
			jq.Match(`.spec.parentRefs[] | select(.name == "%s") | .namespace == "%s"`, tc.gatewayName(), tc.gatewayNamespace()),
			jq.Match(`.spec.parentRefs[] | select(.name == "%s") | .sectionName == null`, tc.gatewayName()),
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

// getExpectedGatewayHostname returns the expected gateway hostname.
// On OpenShift it auto-detects from the cluster Ingress CR; on XKS it reads
// GatewayConfig.spec.domain — mirroring the controller's GetFQDN logic.
// Result is cached to avoid multiple cluster API calls.
func (tc *GatewayTestCtx) getExpectedGatewayHostname(t *testing.T) string {
	t.Helper()
	tc.once.Do(func() {
		gatewayConfig := &serviceApi.GatewayConfig{}
		if err := tc.Client().Get(tc.Context(), types.NamespacedName{Name: gatewayConfigName}, gatewayConfig); err != nil {
			tc.cachedGatewayHostname = ""
			return
		}
		hostname, err := gateway.GetFQDN(tc.Context(), tc.Client(), gatewayConfig)
		if err != nil {
			tc.cachedGatewayHostname = ""
			return
		}
		tc.cachedGatewayHostname = hostname
	})
	if tc.cachedGatewayHostname == "" {
		require.FailNow(t, "failed to determine gateway hostname (check GatewayConfig domain or cluster Ingress CR)")
	}
	t.Logf("Expected gateway hostname: %s", tc.cachedGatewayHostname)
	return tc.cachedGatewayHostname
}

// getIngressMode returns the ingress mode from GatewayConfig.
// Result is cached to avoid multiple cluster API calls.
func (tc *GatewayTestCtx) getIngressMode(t *testing.T) serviceApi.IngressMode {
	t.Helper()
	tc.ingressModeOnce.Do(func() {
		gatewayConfig := &serviceApi.GatewayConfig{}
		err := tc.Client().Get(tc.Context(), types.NamespacedName{Name: gatewayConfigName}, gatewayConfig)
		if err != nil {
			tc.cachedIngressMode = serviceApi.IngressModeOcpRoute
			t.Logf("GatewayConfig not found, defaulting to ingress mode: %s", tc.cachedIngressMode)
			return
		}
		tc.cachedIngressMode = gatewayConfig.Spec.IngressMode
		if tc.cachedIngressMode == "" {
			tc.cachedIngressMode = serviceApi.IngressModeOcpRoute
		}
		t.Logf("Detected ingress mode: %s", tc.cachedIngressMode)
	})
	return tc.cachedIngressMode
}

// isOcpRouteMode returns true if the gateway is configured for OCP Route ingress mode.
func (tc *GatewayTestCtx) isOcpRouteMode(t *testing.T) bool {
	t.Helper()
	return tc.getIngressMode(t) == serviceApi.IngressModeOcpRoute
}

// getTLSSecretName returns the appropriate TLS secret name based on ingress mode.
func (tc *GatewayTestCtx) getTLSSecretName(t *testing.T) string {
	t.Helper()
	if tc.isOcpRouteMode(t) {
		return gatewayServiceTLSSecretName
	}
	return gatewayTLSSecretName
}

// validateOCPRoute validates the OpenShift Route exists and is properly configured for OcpRoute mode.
func (tc *GatewayTestCtx) validateOCPRoute(t *testing.T) {
	t.Helper()
	t.Log("Validating OCP Route for Gateway")

	expectedHostname := tc.getExpectedGatewayHostname(t)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Route, types.NamespacedName{
			Name:      tc.gatewayName(),
			Namespace: tc.gatewayNamespace(),
		}),
		WithCondition(And(
			jq.Match(`.spec.host == "%s"`, expectedHostname),
			jq.Match(`.spec.to.kind == "Service"`),
			jq.Match(`.spec.to.name == "%s"`, gateway.GetDefaultGatewayServiceFullName()),
			jq.Match(`.spec.port.targetPort == %d`, standardHTTPSPort),
			jq.Match(`.spec.tls.termination == "reencrypt"`),
			jq.Match(`.spec.tls.insecureEdgeTerminationPolicy == "Redirect"`),
		)),
		WithCustomErrorMsg("OCP Route should exist with correct configuration for hostname %s", expectedHostname),
	)

	t.Log("OCP Route validation completed")
}

// getOIDCConfig returns the OIDC configuration from GatewayConfig.
// Result is cached to avoid multiple cluster API calls.
func (tc *GatewayTestCtx) getOIDCConfig(t *testing.T) *serviceApi.OIDCConfig {
	t.Helper()
	tc.oidcConfigOnce.Do(func() {
		gatewayConfig := &serviceApi.GatewayConfig{}
		err := tc.Client().Get(tc.Context(), types.NamespacedName{Name: gatewayConfigName}, gatewayConfig)
		require.NoError(t, err, "Failed to get GatewayConfig")
		require.NotNil(t, gatewayConfig.Spec.OIDC, "GatewayConfig should have OIDC configuration on BYOIDC cluster")
		tc.cachedOIDCConfig = gatewayConfig.Spec.OIDC
	})
	return tc.cachedOIDCConfig
}

// ValidateOIDCProxySecret validates that the proxy credentials secret exists on BYOIDC clusters.
// Unlike IntegratedOAuth, no OAuthClient is created; credentials come from the external OIDC provider.
func (tc *GatewayTestCtx) ValidateOIDCProxySecret(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)
	tc.SkipUnlessBYOIDC(t)
	t.Log("Validating OIDC proxy credentials secret")

	// The proxy credentials secret should still exist with the expected keys
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Secret, types.NamespacedName{
			Name:      kubeAuthProxyCredsName,
			Namespace: tc.gatewayNamespace(),
		}),
		WithCondition(And(
			jq.Match(`.type == "%s"`, string(corev1.SecretTypeOpaque)),
			jq.Match(`.metadata.labels["%s"] == "%s"`, labels.PlatformPartOf, gateway.PartOfGatewayConfig),
			jq.Match(`.data | has("%s")`, gateway.EnvClientID),
			jq.Match(`.data | has("%s")`, gateway.EnvClientSecret),
			jq.Match(`.data | has("%s")`, gateway.EnvCookieSecret),
			jq.Match(`.data["%s"] | length > 0`, gateway.EnvClientSecret),
			jq.Match(`.data["%s"] | length > 0`, gateway.EnvCookieSecret),
		)),
		WithCustomErrorMsg("OIDC proxy credentials secret should be Opaque type with required keys"),
	)

	t.Log("OIDC proxy credentials secret validation completed")
}

// ValidateOIDCAuthProxyDeployment validates the kube-auth-proxy deployment on BYOIDC clusters.
// The deployment uses --provider=oidc with OIDC-specific args instead of --provider=openshift.
func (tc *GatewayTestCtx) ValidateOIDCAuthProxyDeployment(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)
	tc.SkipUnlessBYOIDC(t)
	t.Log("Validating kube-auth-proxy OIDC deployment and service")

	expectedGatewayHostname := tc.getExpectedGatewayHostname(t)
	expectedRedirectURL := makeRedirectURL(expectedGatewayHostname)
	expectedCookieDomain := makeCookieDomain(expectedGatewayHostname)
	oidcConfig := tc.getOIDCConfig(t)
	tlsMinVersionArg, tlsCipherSuitesArg, tlsCurvePreferencesArg := tc.expectedKubeAuthProxyTLSDeploymentArgs(t)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      kubeAuthProxyName,
			Namespace: tc.gatewayNamespace(),
		}),
		WithCondition(And(
			// replica count
			jq.Match(`.spec.replicas == 2`),

			// basic pod template checks
			jq.Match(`.spec.selector.matchLabels.app == "%s"`, kubeAuthProxyName),
			jq.Match(`.spec.template.spec.containers | length > 0`),
			jq.Match(`.spec.template.spec.containers[0].name == "%s"`, kubeAuthProxyName),

			// pod security context checks
			jq.Match(`.spec.template.spec.securityContext.runAsNonRoot == true`),
			jq.Match(`.spec.template.spec.securityContext.seccompProfile.type == "RuntimeDefault"`),

			// container security context checks
			jq.Match(`.spec.template.spec.containers[0].securityContext.readOnlyRootFilesystem == true`),
			jq.Match(`.spec.template.spec.containers[0].securityContext.allowPrivilegeEscalation == false`),
			jq.Match(`.spec.template.spec.containers[0].securityContext.capabilities.drop | length > 0`),
			jq.Match(`.spec.template.spec.containers[0].securityContext.capabilities.drop[] | . == "ALL"`),

			// ports
			jq.Match(`.spec.template.spec.containers[0].ports | length == 3`),
			jq.Match(`.spec.template.spec.containers[0].ports[] | select(.name == "http") | .containerPort == %d`, kubeAuthProxyHTTPPort),
			jq.Match(`.spec.template.spec.containers[0].ports[] | select(.name == "https") | .containerPort == %d`, kubeAuthProxyHTTPSPort),
			jq.Match(`.spec.template.spec.containers[0].ports[] | select(.name == "metrics") | .containerPort == %d`, kubeAuthProxyMetricsPort),

			// env from secret
			jq.Match(`.spec.template.spec.containers[0].env[] | select(.name == "%s") | .valueFrom.secretKeyRef.name == "%s"`, gateway.EnvClientID, kubeAuthProxyCredsName),
			jq.Match(`.spec.template.spec.containers[0].env[] | select(.name == "%s") | .valueFrom.secretKeyRef.name == "%s"`, gateway.EnvClientSecret, kubeAuthProxyCredsName),
			jq.Match(`.spec.template.spec.containers[0].env[] | select(.name == "%s") | .valueFrom.secretKeyRef.name == "%s"`, gateway.EnvCookieSecret, kubeAuthProxyCredsName),

			// TLS volume mount
			jq.Match(`.spec.template.spec.containers[0].volumeMounts[] | select(.name == "tls-certs") | .mountPath == "/etc/tls/private"`),
			jq.Match(`.spec.template.spec.containers[0].volumeMounts[] | select(.name == "tls-certs") | .readOnly == true`),
			jq.Match(`.spec.template.spec.volumes[] | select(.name == "tls-certs") | .secret.secretName == "%s"`, kubeAuthProxyTLSName),

			// /tmp volume mount
			jq.Match(`.spec.template.spec.containers[0].volumeMounts[] | select(.name == "tmp") | .mountPath == "/tmp"`),
			jq.Match(`.spec.template.spec.volumes[] | select(.name == "tmp") | .emptyDir.medium == "Memory"`),
			jq.Match(`.spec.template.spec.volumes[] | select(.name == "tmp") | .emptyDir.sizeLimit == "10Mi"`),

			// OIDC-specific args (instead of --provider=openshift / --scope=user:full)
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--provider=oidc")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--oidc-issuer-url=%s")`, oidcConfig.IssuerURL),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--skip-oidc-discovery=false")`),

			// common args
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "%s")`, expectedRedirectURL),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "%s")`, expectedCookieDomain),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--https-address=0.0.0.0:%d")`, kubeAuthProxyHTTPSPort),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--http-address=0.0.0.0:%d")`, kubeAuthProxyHTTPPort),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--tls-cert-file=/etc/tls/private/tls.crt")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--tls-key-file=/etc/tls/private/tls.key")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "%s")`, tlsMinVersionArg),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "%s")`, tlsCipherSuitesArg),
			kubeAuthProxyCurvePreferencesMatcher(tlsCurvePreferencesArg),

			// cookie config
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--cookie-secure=true")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--cookie-httponly=true")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--cookie-samesite=lax")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--cookie-name=_oauth2_proxy")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--cookie-expire=24h0m0s")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--cookie-refresh=1h0m0s")`),

			// auth proxy behavior flags
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--skip-provider-button")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--skip-jwt-bearer-tokens=true")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--pass-authorization-header=true")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--set-authorization-header=true")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--set-xauthrequest=true")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--email-domain=*")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--upstream=static://200")`),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--enable-k8s-token-validation=true")`),

			// OIDC mode must NOT have --pass-access-token (uses id_token via Authorization header instead)
			jq.Match(`.spec.template.spec.containers[0].args | all(. != "--pass-access-token=true")`),

			// metrics and trust store
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--metrics-address=0.0.0.0:%d")`, kubeAuthProxyMetricsPort),
			jq.Match(`.spec.template.spec.containers[0].args | any(. == "--use-system-trust-store=true")`),

			// secret hash annotation
			jq.Match(`.spec.template.metadata.annotations["opendatahub.io/secret-hash"] != null`),
			jq.Match(`.spec.template.metadata.annotations["opendatahub.io/secret-hash"] | test("^[0-9a-f]{64}$|^$")`),
		)),
		WithCustomErrorMsg("kube-auth-proxy OIDC deployment should exist with correct configuration"),
	)

	// Wait for deployment readiness. On macOS/arm64 KinD this may fail because
	// odh-kube-auth-proxy is amd64-only (ImagePullBackOff); amd64 CI with Dex succeeds.
	tc.EnsureDeploymentReady(types.NamespacedName{Name: kubeAuthProxyName, Namespace: tc.gatewayNamespace()}, 2)

	// kube-auth-proxy service
	serviceCondition := And(
		jq.Match(`.spec.selector.app == "%s"`, kubeAuthProxyName),
		jq.Match(`.spec.ports | length == 2`),
		jq.Match(`.spec.ports[] | select(.name == "https") | .port == %d`, kubeAuthProxyHTTPSPort),
		jq.Match(`.spec.ports[] | select(.name == "https") | .targetPort == %d`, kubeAuthProxyHTTPSPort),
		jq.Match(`.spec.ports[] | select(.name == "metrics") | .port == %d`, kubeAuthProxyMetricsPort),
	)

	if !tc.IsXKS() {
		serviceCondition = And(
			serviceCondition,
			jq.Match(`.metadata.annotations."service.beta.openshift.io/serving-cert-secret-name" == "%s"`, kubeAuthProxyTLSName),
		)
	}

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Service, types.NamespacedName{
			Name:      kubeAuthProxyName,
			Namespace: tc.gatewayNamespace(),
		}),
		WithCondition(serviceCondition),
		WithCustomErrorMsg("kube-auth-proxy service should exist with HTTPS and metrics ports"),
	)

	// TLS secret for auth proxy
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Secret, types.NamespacedName{
			Name:      kubeAuthProxyTLSName,
			Namespace: tc.gatewayNamespace(),
		}),
		WithCustomErrorMsg("kube-auth-proxy TLS secret should exist"),
	)

	t.Log("kube-auth-proxy OIDC deployment and service validation completed")
}

// ValidateOIDCUnauthenticatedRedirect tests that unauthenticated requests are redirected to the OIDC provider.
func (tc *GatewayTestCtx) ValidateOIDCUnauthenticatedRedirect(t *testing.T) {
	t.Helper()

	tc.SkipIfXKSCluster(t)
	skipUnless(t, Tier1)
	tc.SkipUnlessBYOIDC(t)

	oidcConfig := tc.getOIDCConfig(t)

	tc.UpdateComponentStateInDataScienceClusterWithKind(operatorv1.Managed, componentApi.DashboardKind)
	defer tc.UpdateComponentStateInDataScienceClusterWithKind(operatorv1.Removed, componentApi.DashboardKind)

	tc.waitForDashboardHTTPRoute(t)
	dashboardURL := tc.getDashboardURL(t)

	t.Log("Testing unauthenticated access on BYOIDC cluster")

	httpClient := tc.createHTTPClient()

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

	// Validate redirect location points to the OIDC issuer's host.
	// We compare hosts rather than checking for a substring because some providers (e.g. Entra ID)
	// use a different path for the authorize endpoint than the issuer URL
	// (issuer: .../v2.0, authorize: .../oauth2/v2.0/authorize).
	location := resp.Header.Get("Location")
	tc.g.Expect(location).NotTo(BeEmpty(), "Redirect response should have Location header")

	issuerURL, err := url.Parse(oidcConfig.IssuerURL)
	tc.g.Expect(err).NotTo(HaveOccurred(), "Failed to parse issuer URL")
	redirectURL, err := url.Parse(location)
	tc.g.Expect(err).NotTo(HaveOccurred(), "Failed to parse redirect location URL")
	tc.g.Expect(redirectURL.Host).To(Equal(issuerURL.Host),
		"Redirect host should match OIDC issuer host %s, got: %s", issuerURL.Host, location)

	tc.g.Expect(location).To(ContainSubstring("redirect_uri="),
		"Redirect should have redirect_uri parameter, got: %s", location)

	t.Log("OIDC unauthenticated access correctly redirects to OIDC provider")
}

// ValidateOIDCTokenForwarding tests that a request authenticated with an OIDC id_token
// has the token forwarded to the dashboard backend as x-forwarded-access-token.
// It sends a Bearer token request to the dashboard's /api/k8s endpoint through the gateway
// and verifies the response is not 401 (which would indicate the token was not forwarded).
func (tc *GatewayTestCtx) ValidateOIDCTokenForwarding(t *testing.T) {
	t.Helper()

	tc.SkipIfXKSCluster(t)
	skipUnless(t, Tier1)
	tc.SkipUnlessBYOIDC(t)

	tc.UpdateComponentStateInDataScienceClusterWithKind(operatorv1.Managed, componentApi.DashboardKind)
	defer tc.UpdateComponentStateInDataScienceClusterWithKind(operatorv1.Removed, componentApi.DashboardKind)

	tc.waitForDashboardHTTPRoute(t)

	idToken := tc.getOIDCIDToken(t)
	dashboardURL := tc.getDashboardURL(t)

	// Use /api/k8s proxy endpoint — this requires x-forwarded-access-token to be set
	// by the EnvoyFilter Lua filter. Without it, the dashboard returns 401.
	apiURL := dashboardURL + "/api/k8s/apis"

	t.Logf("Testing OIDC token forwarding to %s", apiURL)

	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				// #nosec G402 -- e2e test environment
				InsecureSkipVerify: true,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	require.NoError(t, err, "Failed to create HTTP request")

	req.Header.Set("Authorization", "Bearer "+idToken)

	resp, err := httpClient.Do(req)
	require.NoError(t, err, "Failed to make authenticated request through gateway")
	defer resp.Body.Close()

	tc.g.Expect(resp.StatusCode).To(Equal(http.StatusOK),
		"Authenticated request with OIDC id_token to /api/k8s/apis should return 200 — "+
			"got %d, which indicates the token is not being correctly forwarded via x-forwarded-access-token", resp.StatusCode)

	t.Log("OIDC token forwarding verified (status 200)")
}

// getOIDCIDToken extracts the id_token from the current kubeconfig's OIDC auth provider.
func (tc *GatewayTestCtx) getOIDCIDToken(t *testing.T) string {
	t.Helper()

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	rawConfig, err := kubeConfig.RawConfig()
	require.NoError(t, err, "Failed to load kubeconfig")

	currentContext := rawConfig.CurrentContext
	require.NotEmpty(t, currentContext, "No current kubeconfig context")

	ctx, ok := rawConfig.Contexts[currentContext]
	require.True(t, ok, "Current context %q not found in kubeconfig", currentContext)

	authInfo, ok := rawConfig.AuthInfos[ctx.AuthInfo]
	require.True(t, ok, "AuthInfo %q not found in kubeconfig", ctx.AuthInfo)
	require.NotNil(t, authInfo.AuthProvider, "AuthInfo %q has no auth provider (expected OIDC)", ctx.AuthInfo)
	require.Equal(t, "oidc", authInfo.AuthProvider.Name, "Auth provider should be OIDC")

	idToken := authInfo.AuthProvider.Config["id-token"]
	require.NotEmpty(t, idToken, "No id-token found in kubeconfig OIDC auth provider config")

	t.Log("Extracted OIDC id_token from kubeconfig")
	return idToken
}

// getServiceFQDN returns the fully qualified domain name for a Kubernetes service.
// Used to construct service addresses for EnvoyFilter configuration.
// Format: <service-name>.<namespace>.svc.cluster.local.
func getServiceFQDN(serviceName, namespace string) string {
	return fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, namespace)
}

// ValidateNetworkPolicy validates the NetworkPolicy resource for kube-auth-proxy.
func (tc *GatewayTestCtx) ValidateNetworkPolicy(t *testing.T) {
	t.Helper()

	skipUnless(t, Tier1)
	if tc.IsXKS() {
		tc.SkipUnlessBYOIDC(t)
	}
	t.Log("Validating NetworkPolicy for kube-auth-proxy")

	policyChecks := []gomegaTypes.GomegaMatcher{
		// Verify the policy is owned by GatewayConfig and selects only proxy pods.
		jq.Match(`.metadata.ownerReferences | any(.kind == "GatewayConfig" and .name == "%s" and .controller == true)`, gatewayConfigName),
		jq.Match(`.metadata.labels."app.kubernetes.io/component" == "authentication"`),
		jq.Match(`.spec.podSelector.matchLabels.app == "%s"`, kubeAuthProxyName),

		// Keep the current egress contract visible until the egress design is resolved.
		jq.Match(`.spec.policyTypes | any(. == "Ingress")`),
		jq.Match(`.spec.policyTypes | any(. == "Egress")`),
		jq.Match(`.spec.egress | length == 1`),
		jq.Match(`.spec.egress[0] == {}`),

		// Only Gateway pods can use the authentication ingress rule on TCP 8443.
		jq.Match(`.spec.ingress | length >= 1`),
		jq.Match(`.spec.ingress[0].from | length == 1`),
		jq.Match(`.spec.ingress[0].ports | length == 1`),
		jq.Match(`.spec.ingress[0].from[0].podSelector.matchExpressions[0].key == "%s"`, labels.GatewayAPI.GatewayName),
		jq.Match(`.spec.ingress[0].from[0].podSelector.matchExpressions[0].operator == "In"`),
		jq.Match(`.spec.ingress[0].from[0].podSelector.matchExpressions[0].values | any(. == "%s")`, tc.gatewayName()),
		jq.Match(`.spec.ingress[0].from[0].namespaceSelector.matchLabels."kubernetes.io/metadata.name" == "%s"`, tc.gatewayNamespace()),
		jq.Match(`.spec.ingress[0].ports[0].port == %d`, kubeAuthProxyHTTPSPort),
		jq.Match(`.spec.ingress[0].ports[0].protocol == "%s"`, string(corev1.ProtocolTCP)),
		jq.Match(`[.spec.ingress[1:][] | select((.ports | length) == 0 or any(.ports[]; .port == %d))] | length == 0`, kubeAuthProxyHTTPSPort),
		jq.Match(`[.spec.ingress[] | select((.ports | length) == 0 or any(.ports[]; .port == %d))] | length == 0`, kubeAuthProxyHTTPPort),
	}
	if !tc.IsXKS() {
		policyChecks = append(policyChecks,
			jq.Match(`.spec.ingress | length == 3`),
			jq.Match(`.spec.ingress[1].from[0].namespaceSelector.matchLabels."kubernetes.io/metadata.name" == "openshift-monitoring"`),
			jq.Match(`.spec.ingress[2].from[0].namespaceSelector.matchLabels."kubernetes.io/metadata.name" == "openshift-user-workload-monitoring"`),
		)
	}

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.NetworkPolicy, types.NamespacedName{
			Name:      kubeAuthProxyName,
			Namespace: tc.gatewayNamespace(),
		}),
		WithCondition(And(policyChecks...)),
		WithCustomErrorMsg("NetworkPolicy should exist with correct ingress and egress rules for kube-auth-proxy"),
	)

	t.Log("NetworkPolicy validation completed")
}
