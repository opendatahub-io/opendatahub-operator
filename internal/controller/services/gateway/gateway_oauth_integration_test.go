//go:build integration

/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gateway_test

import (
	"strings"
	"testing"
	"time"

	oauthv1 "github.com/openshift/api/oauth/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"

	. "github.com/onsi/gomega"
)

// oauthSpec returns the default OAuth GatewayConfig spec (OCP route).
func oauthSpec() serviceApi.GatewayConfigSpec {
	return serviceApi.GatewayConfigSpec{
		IngressMode: serviceApi.IngressModeOcpRoute,
	}
}

// oauthSpecWithSubdomain returns the default OAuth spec with the given subdomain.
func oauthSpecWithSubdomain(subdomain string) serviceApi.GatewayConfigSpec {
	spec := oauthSpec()
	spec.Subdomain = subdomain
	return spec
}

// oauthSpecWithAuthProxyTimeout returns the default OAuth spec with AuthProxyTimeout set.
func oauthSpecWithAuthProxyTimeout(d time.Duration) serviceApi.GatewayConfigSpec {
	spec := oauthSpec()
	spec.AuthProxyTimeout = metav1.Duration{Duration: d}
	return spec
}

// oauthSpecWithNetworkPolicyDisabled returns the default OAuth spec with ingress network policy disabled.
func oauthSpecWithNetworkPolicyDisabled() serviceApi.GatewayConfigSpec {
	spec := oauthSpec()
	spec.NetworkPolicy = &serviceApi.NetworkPolicyConfig{
		Ingress: &serviceApi.IngressPolicyConfig{Enabled: false},
	}
	return spec
}

// oauthSpecWithLoadBalancer returns the default OAuth spec with IngressMode LoadBalancer.
func oauthSpecWithLoadBalancer() serviceApi.GatewayConfigSpec {
	spec := oauthSpec()
	spec.IngressMode = serviceApi.IngressModeLoadBalancer
	return spec
}

// GetOAuthTestSetup returns TestSetup for OAuth mode (OAuthTestEnv, default OCP route spec). Use for most OAuth tests.
func GetOAuthTestSetup() TestSetup {
	return TestSetup{
		TC:   OAuthTestEnv,
		Spec: oauthSpec(),
	}
}

// GetOAuthTestSetupForExtAuthz returns TestSetup with AuthProxyTimeout set (e.g. 45s). Use for ext_authz EnvoyFilter test.
func GetOAuthTestSetupForExtAuthz() TestSetup {
	return TestSetup{
		TC:   OAuthTestEnv,
		Spec: oauthSpecWithAuthProxyTimeout(45 * time.Second),
	}
}

// TestOAuthGatewayClassCreation validates GatewayClass creation in OAuth mode (delegates to RunGatewayClassCreationTest).
func TestOAuthGatewayClassCreation(t *testing.T) {
	RunGatewayClassCreationTest(t, GetOAuthTestSetup())
}

// TestOAuthGatewayCreation validates Gateway creation in OAuth mode (delegates to RunGatewayCreationTest).
func TestOAuthGatewayCreation(t *testing.T) {
	RunGatewayCreationTest(t, GetOAuthTestSetup())
}

// TestOAuthHTTPRouteCreation validates HTTPRoute creation in OAuth mode (delegates to RunHTTPRouteCreationTest).
func TestOAuthHTTPRouteCreation(t *testing.T) {
	RunHTTPRouteCreationTest(t, GetOAuthTestSetup())
}

// TestOAuthOAuthClientCreation validates that OAuthClient is created with correct grant method, secret, and redirect URI when using OAuth mode.
func TestOAuthOAuthClientCreation(t *testing.T) {
	tc := OAuthTestEnv
	g := NewWithT(t)

	CreateGatewayConfig(t, tc.Ctx, tc.K8sClient, oauthSpec())
	defer DeleteGatewayConfig(t, tc.Ctx, tc.K8sClient)

	// Verify OAuthClient is created
	g.Eventually(func() error {
		oc := &oauthv1.OAuthClient{}
		return tc.K8sClient.Get(tc.Ctx, types.NamespacedName{Name: gateway.AuthClientID}, oc)
	}, TestTimeout, TestInterval).Should(Succeed())

	oc := &oauthv1.OAuthClient{}
	g.Expect(tc.K8sClient.Get(tc.Ctx, types.NamespacedName{Name: gateway.AuthClientID}, oc)).To(Succeed())
	g.Expect(oc.GrantMethod).To(Equal(oauthv1.GrantHandlerAuto))
	g.Expect(oc.Secret).NotTo(BeEmpty())

	g.Expect(oc.RedirectURIs).To(ContainElement(OAuthRedirectURI(DefaultGatewayHost(OAuthClusterDomain))))
}

// TestOAuthAuthProxySecretCreation validates auth-proxy secret creation in OAuth mode (delegates to RunAuthProxySecretCreationTest).
func TestOAuthAuthProxySecretCreation(t *testing.T) {
	RunAuthProxySecretCreationTest(t, GetOAuthTestSetup(), "")
}

// TestOAuthServiceCreation validates Service creation in OAuth mode (delegates to RunServiceCreationTest).
func TestOAuthServiceCreation(t *testing.T) {
	RunServiceCreationTest(t, GetOAuthTestSetup())
}

// TestOAuthDeploymentWithAllArgs validates Deployment args in OAuth mode (delegates to RunDeploymentWithAllArgsTest).
func TestOAuthDeploymentWithAllArgs(t *testing.T) {
	RunDeploymentWithAllArgsTest(t, GetOAuthTestSetup(), DefaultGatewayHost(OAuthClusterDomain),
		[]string{"--provider=openshift", "--scope=user:full", "--ssl-insecure-skip-verify=false"},
		nil)
}

// TestOAuthHPACreation validates HPA creation in OAuth mode (delegates to RunHPACreationTest).
func TestOAuthHPACreation(t *testing.T) {
	RunHPACreationTest(t, GetOAuthTestSetup())
}

// TestOAuthEnvoyFilterCreation validates EnvoyFilter creation in OAuth mode (delegates to RunEnvoyFilterCreationTest).
func TestOAuthEnvoyFilterCreation(t *testing.T) {
	RunEnvoyFilterCreationTest(t, GetOAuthTestSetup())
}

// TestOAuthEnvoyFilterExtAuthzConfiguration validates ext_authz EnvoyFilter config in OAuth mode (delegates to RunEnvoyFilterExtAuthzConfigurationTest).
func TestOAuthEnvoyFilterExtAuthzConfiguration(t *testing.T) {
	RunEnvoyFilterExtAuthzConfigurationTest(t, GetOAuthTestSetupForExtAuthz())
}

// TestOAuthEnvoyFilterOrder validates EnvoyFilter order in OAuth mode (delegates to RunEnvoyFilterOrderTest).
func TestOAuthEnvoyFilterOrder(t *testing.T) {
	RunEnvoyFilterOrderTest(t, GetOAuthTestSetup())
}

// TestOAuthEnvoyFilterLuaTokenForwarding validates EnvoyFilter lua token forwarding in OAuth mode (delegates to RunEnvoyFilterLuaTokenForwardingTest).
func TestOAuthEnvoyFilterLuaTokenForwarding(t *testing.T) {
	RunEnvoyFilterLuaTokenForwardingTest(t, GetOAuthTestSetup())
}

// TestOAuthEnvoyFilterLegacyRedirectPresent validates legacy redirect EnvoyFilter in OAuth mode (delegates to RunEnvoyFilterLegacyRedirectPresentTest).
func TestOAuthEnvoyFilterLegacyRedirectPresent(t *testing.T) {
	RunEnvoyFilterLegacyRedirectPresentTest(t, GetOAuthTestSetup())
}

// TestOAuthEnvoyFilterLegacyRedirectOrderFirst validates legacy redirect filter order in OAuth mode (delegates to RunEnvoyFilterLegacyRedirectOrderFirstTest).
func TestOAuthEnvoyFilterLegacyRedirectOrderFirst(t *testing.T) {
	RunEnvoyFilterLegacyRedirectOrderFirstTest(t, GetOAuthTestSetup())
}

// TestOAuthDestinationRuleCreation validates DestinationRule creation in OAuth mode (delegates to RunDestinationRuleCreationTest).
func TestOAuthDestinationRuleCreation(t *testing.T) {
	RunDestinationRuleCreationTest(t, GetOAuthTestSetup())
}

// TestOAuthOCPRouteCreation validates OCP Route creation in OAuth mode (delegates to RunOCPRouteCreationTest).
func TestOAuthOCPRouteCreation(t *testing.T) {
	RunOCPRouteCreationTest(t, GetOAuthTestSetup(), DefaultGatewayHost(OAuthClusterDomain))
}

// TestOAuthLegacyRedirectRouteCreation validates legacy redirect Route creation in OAuth mode (delegates to RunLegacyRedirectRouteCreationTest).
func TestOAuthLegacyRedirectRouteCreation(t *testing.T) {
	RunLegacyRedirectRouteCreationTest(t, GetOAuthTestSetup(), LegacyGatewayHost(OAuthClusterDomain))
}

// TestOAuthNetworkPolicyCreation validates NetworkPolicy creation in OAuth mode (delegates to RunNetworkPolicyCreationTest).
func TestOAuthNetworkPolicyCreation(t *testing.T) {
	RunNetworkPolicyCreationTest(t, GetOAuthTestSetup())
}

// TestOAuthNginxDashboardRedirectCreation validates nginx-based dashboard redirect resources (ConfigMap, Deployment, Service, Routes) in OAuth mode.
func TestOAuthNginxDashboardRedirectCreation(t *testing.T) {
	RunNginxDashboardRedirectCreationTest(t, GetOAuthTestSetup())
}

// TestOAuthLegacyRouteRemovedWhenSubdomainChangesToLegacy validates no legacy redirect route when subdomain is legacy from the start (delegates to RunLegacyRouteRemovedWhenSubdomainChangesToLegacyTest).
func TestOAuthLegacyRouteRemovedWhenSubdomainChangesToLegacy(t *testing.T) {
	RunLegacyRouteRemovedWhenSubdomainChangesToLegacyTest(t, GetOAuthTestSetup())
}

// TestOAuthSpecMutationCookieConfig validates cookie spec mutation in OAuth mode (delegates to RunSpecMutationCookieConfigTest).
func TestOAuthSpecMutationCookieConfig(t *testing.T) {
	RunSpecMutationCookieConfigTest(t, GetOAuthTestSetup(), SpecMutationCookieConfig())
}

// TestOAuthSpecMutationSubdomainChange validates that GatewayConfig subdomain and OAuthClient redirect URIs update when subdomain changes.
func TestOAuthSpecMutationSubdomainChange(t *testing.T) {
	tc := OAuthTestEnv
	g := NewWithT(t)

	CreateGatewayConfig(t, tc.Ctx, tc.K8sClient, oauthSpecWithSubdomain("custom-gateway"))
	defer DeleteGatewayConfig(t, tc.Ctx, tc.K8sClient)

	g.Eventually(func() bool {
		oc := &oauthv1.OAuthClient{}
		if err := tc.K8sClient.Get(tc.Ctx, types.NamespacedName{Name: gateway.AuthClientID}, oc); err != nil {
			return false
		}
		for _, uri := range oc.RedirectURIs {
			if strings.Contains(uri, HostForSubdomain("custom-gateway", OAuthClusterDomain)) {
				return true
			}
		}
		return false
	}, TestTimeout, TestInterval).Should(BeTrue())

	UpdateGatewayConfig(t, tc.Ctx, tc.K8sClient, oauthSpecWithSubdomain("new-subdomain"))

	g.Eventually(func() bool {
		gc := &serviceApi.GatewayConfig{}
		if err := tc.K8sClient.Get(tc.Ctx, types.NamespacedName{Name: serviceApi.GatewayConfigName}, gc); err != nil {
			return false
		}
		if gc.Spec.Subdomain != "new-subdomain" {
			return false
		}
		oc := &oauthv1.OAuthClient{}
		if err := tc.K8sClient.Get(tc.Ctx, types.NamespacedName{Name: gateway.AuthClientID}, oc); err != nil {
			return false
		}
		for _, uri := range oc.RedirectURIs {
			if strings.Contains(uri, HostForSubdomain("new-subdomain", OAuthClusterDomain)) {
				return true
			}
		}
		return false
	}, TestTimeout, TestInterval).Should(BeTrue(), "GatewayConfig subdomain and OAuthClient redirect URIs must be updated")
}

// TestOAuthGatewayConfigStatusConditions validates GatewayConfig status conditions in OAuth mode (delegates to RunGatewayConfigStatusConditionsTest).
func TestOAuthGatewayConfigStatusConditions(t *testing.T) {
	RunGatewayConfigStatusConditionsTest(t, GetOAuthTestSetup())
}

// TestOAuthGatewayConfigStatusDomain validates GatewayConfig status domain in OAuth mode (delegates to RunGatewayConfigStatusDomainTest).
func TestOAuthGatewayConfigStatusDomain(t *testing.T) {
	RunGatewayConfigStatusDomainTest(t, GetOAuthTestSetup(), DefaultGatewayHost(OAuthClusterDomain))
}

// TestOAuthLoadBalancerIngressMode validates LoadBalancer ingress mode in OAuth (delegates to RunLoadBalancerIngressModeTest).
func TestOAuthLoadBalancerIngressMode(t *testing.T) {
	RunLoadBalancerIngressModeTest(t, OAuthTestEnv, oauthSpecWithLoadBalancer())
}

// TestOAuthNetworkPolicyDisabled validates that no NetworkPolicy is created when ingress policy is disabled (delegates to RunNetworkPolicyDisabledTest).
func TestOAuthNetworkPolicyDisabled(t *testing.T) {
	RunNetworkPolicyDisabledTest(t, GetOAuthTestSetup(), oauthSpecWithNetworkPolicyDisabled())
}
