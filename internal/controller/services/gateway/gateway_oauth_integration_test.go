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
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"

	. "github.com/onsi/gomega"
)

const (
	oauthClusterDomain = "apps.oauth-test.example.com"
)

// Note: TestMain is defined in gateway_main_test.go to initialize both OAuth and OIDC environments

// GetOAuthTestSetup returns the test setup for OAuth mode.
func GetOAuthTestSetup() TestSetup {
	return TestSetup{
		TC: OAuthTestEnv,
		Spec: serviceApi.GatewayConfigSpec{
			IngressMode: serviceApi.IngressModeOcpRoute,
		},
	}
}

// GetOAuthTestSetupForExtAuthz returns setup with AuthProxyTimeout for ext_authz tests.
func GetOAuthTestSetupForExtAuthz() TestSetup {
	return TestSetup{
		TC: OAuthTestEnv,
		Spec: serviceApi.GatewayConfigSpec{
			IngressMode:      serviceApi.IngressModeOcpRoute,
			AuthProxyTimeout: metav1.Duration{Duration: 45 * time.Second},
		},
	}
}

func TestOAuthGatewayClassCreation(t *testing.T) {
	RunGatewayClassCreationTest(t, GetOAuthTestSetup())
}

func TestOAuthGatewayCreation(t *testing.T) {
	RunGatewayCreationTest(t, GetOAuthTestSetup())
}

func TestOAuthHTTPRouteCreation(t *testing.T) {
	RunHTTPRouteCreationTest(t, GetOAuthTestSetup())
}

func TestOAuthOAuthClientCreation(t *testing.T) {
	tc := OAuthTestEnv
	g := NewWithT(t)

	CreateGatewayConfig(t, tc.Ctx, tc.K8sClient, serviceApi.GatewayConfigSpec{
		IngressMode: serviceApi.IngressModeOcpRoute,
	})
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

	// Verify redirect URI contains correct hostname
	expectedHostname := gateway.DefaultGatewaySubdomain + "." + oauthClusterDomain
	expectedRedirectURI := "https://" + expectedHostname + gateway.AuthProxyOAuth2Path + "/callback"
	g.Expect(oc.RedirectURIs).To(ContainElement(expectedRedirectURI))
}

func TestOAuthAuthProxySecretCreation(t *testing.T) {
	RunAuthProxySecretCreationTest(t, GetOAuthTestSetup(), "")
}

func TestOAuthServiceCreation(t *testing.T) {
	RunServiceCreationTest(t, GetOAuthTestSetup())
}

func TestOAuthDeploymentWithAllArgs(t *testing.T) {
	expectedHostname := gateway.DefaultGatewaySubdomain + "." + oauthClusterDomain
	RunDeploymentWithAllArgsTest(t, GetOAuthTestSetup(), expectedHostname,
		[]string{"--provider=openshift", "--scope=user:full", "--ssl-insecure-skip-verify=false"},
		nil)
}

func TestOAuthHPACreation(t *testing.T) {
	RunHPACreationTest(t, GetOAuthTestSetup())
}

func TestOAuthEnvoyFilterCreation(t *testing.T) {
	RunEnvoyFilterCreationTest(t, GetOAuthTestSetup())
}

func TestOAuthEnvoyFilterExtAuthzConfiguration(t *testing.T) {
	RunEnvoyFilterExtAuthzConfigurationTest(t, GetOAuthTestSetupForExtAuthz())
}

func TestOAuthEnvoyFilterOrder(t *testing.T) {
	RunEnvoyFilterOrderTest(t, GetOAuthTestSetup())
}

func TestOAuthEnvoyFilterLuaTokenForwarding(t *testing.T) {
	RunEnvoyFilterLuaTokenForwardingTest(t, GetOAuthTestSetup())
}

func TestOAuthEnvoyFilterLegacyRedirectPresent(t *testing.T) {
	RunEnvoyFilterLegacyRedirectPresentTest(t, GetOAuthTestSetup())
}

func TestOAuthEnvoyFilterLegacyRedirectOrderFirst(t *testing.T) {
	RunEnvoyFilterLegacyRedirectOrderFirstTest(t, GetOAuthTestSetup())
}

func TestOAuthDestinationRuleCreation(t *testing.T) {
	RunDestinationRuleCreationTest(t, GetOAuthTestSetup())
}

func TestOAuthOCPRouteCreation(t *testing.T) {
	RunOCPRouteCreationTest(t, GetOAuthTestSetup(), gateway.DefaultGatewaySubdomain+"."+oauthClusterDomain)
}

func TestOAuthLegacyRedirectRouteCreation(t *testing.T) {
	RunLegacyRedirectRouteCreationTest(t, GetOAuthTestSetup(), gateway.LegacyGatewaySubdomain+"."+oauthClusterDomain)
}

func TestOAuthNetworkPolicyCreation(t *testing.T) {
	RunNetworkPolicyCreationTest(t, GetOAuthTestSetup())
}

func TestOAuthLegacyRouteRemovedWhenSubdomainChangesToLegacy(t *testing.T) {
	// Skip: GC (garbage collection) behavior doesn't work reliably in envtest.
	// The GC action runs but doesn't delete resources because envtest lacks full
	// RBAC/discovery capabilities that the GC relies on. This behavior works correctly
	// in production - verified manually.
	// TODO: Consider E2E test for GC behavior verification.
	t.Skip("Skipping: GC behavior not reliable in envtest - works in production")

	RunLegacyRouteRemovedWhenSubdomainChangesToLegacyTest(t, GetOAuthTestSetup())
}

func TestOAuthSpecMutationCookieConfig(t *testing.T) {
	RunSpecMutationCookieConfigTest(t, GetOAuthTestSetup(), serviceApi.CookieConfig{
		Expire:  metav1.Duration{Duration: 48 * time.Hour},
		Refresh: metav1.Duration{Duration: 2 * time.Hour},
	})
}

func TestOAuthSpecMutationSubdomainChange(t *testing.T) {
	tc := OAuthTestEnv
	g := NewWithT(t)

	// Create with custom subdomain
	CreateGatewayConfig(t, tc.Ctx, tc.K8sClient, serviceApi.GatewayConfigSpec{
		IngressMode: serviceApi.IngressModeOcpRoute,
		Subdomain:   "custom-gateway",
	})
	defer DeleteGatewayConfig(t, tc.Ctx, tc.K8sClient)

	// Wait for OAuthClient with custom subdomain
	customHostname := "custom-gateway." + oauthClusterDomain
	g.Eventually(func() bool {
		oc := &oauthv1.OAuthClient{}
		if err := tc.K8sClient.Get(tc.Ctx, types.NamespacedName{Name: gateway.AuthClientID}, oc); err != nil {
			return false
		}
		for _, uri := range oc.RedirectURIs {
			if strings.Contains(uri, customHostname) {
				return true
			}
		}
		return false
	}, TestTimeout, TestInterval).Should(BeTrue())

	// Update subdomain
	UpdateGatewayConfig(t, tc.Ctx, tc.K8sClient, serviceApi.GatewayConfigSpec{
		IngressMode: serviceApi.IngressModeOcpRoute,
		Subdomain:   "new-subdomain",
	})

	// Verify OAuthClient is updated
	newHostname := "new-subdomain." + oauthClusterDomain
	g.Eventually(func() bool {
		oc := &oauthv1.OAuthClient{}
		if err := tc.K8sClient.Get(tc.Ctx, types.NamespacedName{Name: gateway.AuthClientID}, oc); err != nil {
			return false
		}
		for _, uri := range oc.RedirectURIs {
			if strings.Contains(uri, newHostname) {
				return true
			}
		}
		return false
	}, TestTimeout, TestInterval).Should(BeTrue())
}

func TestOAuthGatewayConfigStatusConditions(t *testing.T) {
	RunGatewayConfigStatusConditionsTest(t, GetOAuthTestSetup())
}

func TestOAuthGatewayConfigStatusDomain(t *testing.T) {
	RunGatewayConfigStatusDomainTest(t, GetOAuthTestSetup(), gateway.DefaultGatewaySubdomain+"."+oauthClusterDomain)
}

func TestOAuthLoadBalancerIngressMode(t *testing.T) {
	// Skip: LoadBalancer mode requires IngressController CRD which is not available in envtest.
	// The controller tries to propagate default ingress certificate which needs IngressController resource.
	// This test would require additional OpenShift operator CRDs to be mocked.
	t.Skip("Skipping LoadBalancer test - requires IngressController CRD not available in envtest")

	tc := OAuthTestEnv
	g := NewWithT(t)

	CreateGatewayConfig(t, tc.Ctx, tc.K8sClient, serviceApi.GatewayConfigSpec{
		IngressMode: serviceApi.IngressModeLoadBalancer,
	})
	defer DeleteGatewayConfig(t, tc.Ctx, tc.K8sClient)

	// Verify Service is created with LoadBalancer type
	g.Eventually(func() bool {
		svc := &corev1.Service{}
		if err := tc.K8sClient.Get(tc.Ctx, types.NamespacedName{
			Name:      gateway.KubeAuthProxyName,
			Namespace: gateway.GatewayNamespace,
		}, svc); err != nil {
			return false
		}
		return svc.Spec.Type == corev1.ServiceTypeLoadBalancer
	}, TestTimeout, TestInterval).Should(BeTrue(), "Service should be LoadBalancer type")

	// Verify NO OCP Route is created in LoadBalancer mode
	g.Consistently(func() bool {
		route := &routev1.Route{}
		err := tc.K8sClient.Get(tc.Ctx, types.NamespacedName{
			Name:      gateway.DefaultGatewayName,
			Namespace: gateway.GatewayNamespace,
		}, route)
		return client.IgnoreNotFound(err) == nil && err != nil
	}, 3*time.Second, TestInterval).Should(BeTrue(), "OCP Route should NOT be created in LoadBalancer mode")
}

func TestOAuthNetworkPolicyDisabled(t *testing.T) {
	spec := serviceApi.GatewayConfigSpec{
		IngressMode: serviceApi.IngressModeOcpRoute,
		NetworkPolicy: &serviceApi.NetworkPolicyConfig{
			Ingress: &serviceApi.IngressPolicyConfig{Enabled: false},
		},
	}
	RunNetworkPolicyDisabledTest(t, GetOAuthTestSetup(), spec)
}
