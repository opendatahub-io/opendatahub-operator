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
	"fmt"
	"testing"
	"time"

	oauthv1 "github.com/openshift/api/oauth/v1"
	routev1 "github.com/openshift/api/route/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"

	. "github.com/onsi/gomega"
)

// ensureOIDCClientSecret creates the OIDC client secret in the gateway namespace if missing. Idempotent; used as TestSetup.SetupFunc.
func ensureOIDCClientSecret(t *testing.T, tc *TestEnvContext) {
	t.Helper()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      OIDCSecretName,
			Namespace: gateway.GatewayNamespace,
		},
		Data: map[string][]byte{
			OIDCSecretKey: []byte("test-client-secret"),
		},
	}
	if err := tc.K8sClient.Create(tc.Ctx, secret); err != nil && !k8serr.IsAlreadyExists(err) {
		t.Fatalf("Failed to create OIDC client secret: %v", err)
	}
}

// getOIDCGatewayConfigSpec returns the default OIDC GatewayConfig spec (issuer, client ID, secret ref).
func getOIDCGatewayConfigSpec() serviceApi.GatewayConfigSpec {
	return serviceApi.GatewayConfigSpec{
		IngressMode: serviceApi.IngressModeOcpRoute,
		OIDC: &serviceApi.OIDCConfig{
			IssuerURL: OIDCIssuerURL,
			ClientID:  OIDCClientID,
			ClientSecretRef: corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: OIDCSecretName,
				},
				Key: OIDCSecretKey,
			},
		},
	}
}

// oidcSpecWithLoadBalancer returns the OIDC spec with IngressMode LoadBalancer.
func oidcSpecWithLoadBalancer() serviceApi.GatewayConfigSpec {
	spec := getOIDCGatewayConfigSpec()
	spec.IngressMode = serviceApi.IngressModeLoadBalancer
	return spec
}

// oidcSpecWithNetworkPolicyDisabled returns the OIDC spec with ingress network policy disabled.
func oidcSpecWithNetworkPolicyDisabled() serviceApi.GatewayConfigSpec {
	spec := getOIDCGatewayConfigSpec()
	spec.NetworkPolicy = &serviceApi.NetworkPolicyConfig{
		Ingress: &serviceApi.IngressPolicyConfig{Enabled: false},
	}
	return spec
}

// oidcSpecWithProviderCA returns the OIDC spec with ProviderCASecretName set.
func oidcSpecWithProviderCA(secretName string) serviceApi.GatewayConfigSpec {
	spec := getOIDCGatewayConfigSpec()
	spec.ProviderCASecretName = secretName
	return spec
}

// oidcSpecWithVerifyProviderCertificate returns the OIDC spec with VerifyProviderCertificate set.
func oidcSpecWithVerifyProviderCertificate(verify bool) serviceApi.GatewayConfigSpec {
	spec := getOIDCGatewayConfigSpec()
	spec.VerifyProviderCertificate = &verify
	return spec
}

// oidcSpecWithAuthProxyTimeout returns the OIDC spec with AuthProxyTimeout set.
func oidcSpecWithAuthProxyTimeout(d time.Duration) serviceApi.GatewayConfigSpec {
	spec := getOIDCGatewayConfigSpec()
	spec.AuthProxyTimeout = metav1.Duration{Duration: d}
	return spec
}

// oidcSpecWithSubdomain returns the OIDC spec with the given subdomain.
func oidcSpecWithSubdomain(subdomain string) serviceApi.GatewayConfigSpec {
	spec := getOIDCGatewayConfigSpec()
	spec.Subdomain = subdomain
	return spec
}

// oidcSpecWithIssuerURL returns the OIDC spec with the given issuer URL.
func oidcSpecWithIssuerURL(issuerURL string) serviceApi.GatewayConfigSpec {
	spec := getOIDCGatewayConfigSpec()
	spec.OIDC.IssuerURL = issuerURL
	return spec
}

// GetOIDCTestSetup returns TestSetup for OIDC mode (OIDCTestEnv, default OIDC spec, ensureOIDCClientSecret). Use for most OIDC tests.
func GetOIDCTestSetup() TestSetup {
	return TestSetup{
		TC:        OIDCTestEnv,
		Spec:      getOIDCGatewayConfigSpec(),
		SetupFunc: ensureOIDCClientSecret,
	}
}

// GetOIDCTestSetupForExtAuthz returns TestSetup with AuthProxyTimeout set and OIDC client secret. Use for ext_authz EnvoyFilter test.
func GetOIDCTestSetupForExtAuthz() TestSetup {
	return TestSetup{
		TC:        OIDCTestEnv,
		Spec:      oidcSpecWithAuthProxyTimeout(45 * time.Second),
		SetupFunc: ensureOIDCClientSecret,
	}
}

// TestOIDCGatewayClassCreation validates GatewayClass creation in OIDC mode (delegates to RunGatewayClassCreationTest).
func TestOIDCGatewayClassCreation(t *testing.T) {
	RunGatewayClassCreationTest(t, GetOIDCTestSetup())
}

// TestOIDCGatewayCreation validates Gateway creation in OIDC mode (delegates to RunGatewayCreationTest).
func TestOIDCGatewayCreation(t *testing.T) {
	RunGatewayCreationTest(t, GetOIDCTestSetup())
}

// TestOIDCHTTPRouteCreation validates HTTPRoute creation in OIDC mode (delegates to RunHTTPRouteCreationTest).
func TestOIDCHTTPRouteCreation(t *testing.T) {
	RunHTTPRouteCreationTest(t, GetOIDCTestSetup())
}

// TestOIDCNoOAuthClientCreation validates that no OAuthClient is created when using OIDC.
func TestOIDCNoOAuthClientCreation(t *testing.T) {
	tc := OIDCTestEnv
	g := NewWithT(t)

	ensureOIDCClientSecret(t, tc)
	CreateGatewayConfig(t, tc.Ctx, tc.K8sClient, getOIDCGatewayConfigSpec())
	defer DeleteGatewayConfig(t, tc.Ctx, tc.K8sClient)

	// Wait for Deployment to be created (indicates controller has processed)
	g.Eventually(func() error {
		deployment := &appsv1.Deployment{}
		return tc.K8sClient.Get(tc.Ctx, types.NamespacedName{
			Name:      gateway.KubeAuthProxyName,
			Namespace: gateway.GatewayNamespace,
		}, deployment)
	}, TestTimeout, TestInterval).Should(Succeed())

	// Verify OAuthClient is NOT created in OIDC mode
	g.Consistently(func() bool {
		oc := &oauthv1.OAuthClient{}
		err := tc.K8sClient.Get(tc.Ctx, types.NamespacedName{Name: gateway.AuthClientID}, oc)
		return client.IgnoreNotFound(err) == nil && err != nil
	}, 5*time.Second, TestInterval).Should(BeTrue())
}

// TestOIDCAuthProxySecretCreation validates auth-proxy secret creation in OIDC mode (delegates to RunAuthProxySecretCreationTest).
func TestOIDCAuthProxySecretCreation(t *testing.T) {
	RunAuthProxySecretCreationTest(t, GetOIDCTestSetup(), OIDCClientID)
}

// TestOIDCServiceCreation validates Service creation in OIDC mode (delegates to RunServiceCreationTest).
func TestOIDCServiceCreation(t *testing.T) {
	RunServiceCreationTest(t, GetOIDCTestSetup())
}

// TestOIDCDeploymentWithAllArgs validates Deployment args in OIDC mode (delegates to RunDeploymentWithAllArgsTest).
func TestOIDCDeploymentWithAllArgs(t *testing.T) {
	RunDeploymentWithAllArgsTest(t, GetOIDCTestSetup(), DefaultGatewayHost(OIDCClusterDomain),
		[]string{"--provider=oidc", fmt.Sprintf("--oidc-issuer-url=%s", OIDCIssuerURL), "--skip-oidc-discovery=false", "--ssl-insecure-skip-verify=false"},
		[]string{"--provider=openshift", "--scope=user:full"})
}

// TestOIDCHPACreation validates HPA creation in OIDC mode (delegates to RunHPACreationTest).
func TestOIDCHPACreation(t *testing.T) {
	RunHPACreationTest(t, GetOIDCTestSetup())
}

// TestOIDCEnvoyFilterCreation validates EnvoyFilter creation in OIDC mode (delegates to RunEnvoyFilterCreationTest).
func TestOIDCEnvoyFilterCreation(t *testing.T) {
	RunEnvoyFilterCreationTest(t, GetOIDCTestSetup())
}

// TestOIDCEnvoyFilterExtAuthzConfiguration validates ext_authz EnvoyFilter config in OIDC mode (delegates to RunEnvoyFilterExtAuthzConfigurationTest).
func TestOIDCEnvoyFilterExtAuthzConfiguration(t *testing.T) {
	RunEnvoyFilterExtAuthzConfigurationTest(t, GetOIDCTestSetupForExtAuthz())
}

// TestOIDCEnvoyFilterOrder validates EnvoyFilter order in OIDC mode (delegates to RunEnvoyFilterOrderTest).
func TestOIDCEnvoyFilterOrder(t *testing.T) {
	RunEnvoyFilterOrderTest(t, GetOIDCTestSetup())
}

// TestOIDCEnvoyFilterLuaTokenForwarding validates EnvoyFilter lua token forwarding in OIDC mode (delegates to RunEnvoyFilterLuaTokenForwardingTest).
func TestOIDCEnvoyFilterLuaTokenForwarding(t *testing.T) {
	RunEnvoyFilterLuaTokenForwardingTest(t, GetOIDCTestSetup())
}

// TestOIDCEnvoyFilterLegacyRedirectPresent validates legacy redirect EnvoyFilter in OIDC mode (delegates to RunEnvoyFilterLegacyRedirectPresentTest).
func TestOIDCEnvoyFilterLegacyRedirectPresent(t *testing.T) {
	RunEnvoyFilterLegacyRedirectPresentTest(t, GetOIDCTestSetup())
}

// TestOIDCEnvoyFilterLegacyRedirectOrderFirst validates legacy redirect filter order in OIDC mode (delegates to RunEnvoyFilterLegacyRedirectOrderFirstTest).
func TestOIDCEnvoyFilterLegacyRedirectOrderFirst(t *testing.T) {
	RunEnvoyFilterLegacyRedirectOrderFirstTest(t, GetOIDCTestSetup())
}

// TestOIDCDestinationRuleCreation validates DestinationRule creation in OIDC mode (delegates to RunDestinationRuleCreationTest).
func TestOIDCDestinationRuleCreation(t *testing.T) {
	RunDestinationRuleCreationTest(t, GetOIDCTestSetup())
}

// TestOIDCOCPRouteCreation validates OCP Route creation in OIDC mode (delegates to RunOCPRouteCreationTest).
func TestOIDCOCPRouteCreation(t *testing.T) {
	RunOCPRouteCreationTest(t, GetOIDCTestSetup(), DefaultGatewayHost(OIDCClusterDomain))
}

// TestOIDCLegacyRedirectRouteCreation validates legacy redirect Route creation in OIDC mode (delegates to RunLegacyRedirectRouteCreationTest).
func TestOIDCLegacyRedirectRouteCreation(t *testing.T) {
	RunLegacyRedirectRouteCreationTest(t, GetOIDCTestSetup(), LegacyGatewayHost(OIDCClusterDomain))
}

// TestOIDCLegacyRouteRemovedWhenSubdomainChangesToLegacy validates no legacy redirect route when subdomain is legacy from the start (delegates to RunLegacyRouteRemovedWhenSubdomainChangesToLegacyTest).
func TestOIDCLegacyRouteRemovedWhenSubdomainChangesToLegacy(t *testing.T) {
	RunLegacyRouteRemovedWhenSubdomainChangesToLegacyTest(t, GetOIDCTestSetup())
}

// TestOIDCNetworkPolicyCreation validates NetworkPolicy creation in OIDC mode (delegates to RunNetworkPolicyCreationTest).
func TestOIDCNetworkPolicyCreation(t *testing.T) {
	RunNetworkPolicyCreationTest(t, GetOIDCTestSetup())
}

// TestOIDCNetworkPolicyDisabled validates that no NetworkPolicy is created when ingress policy is disabled in OIDC mode (delegates to RunNetworkPolicyDisabledTest).
func TestOIDCNetworkPolicyDisabled(t *testing.T) {
	ensureOIDCClientSecret(t, OIDCTestEnv)
	RunNetworkPolicyDisabledTest(t, GetOIDCTestSetup(), oidcSpecWithNetworkPolicyDisabled())
}

// TestOIDCWithProviderCASecret validates that Deployment gets provider CA volume, mount, and --provider-ca-file arg when ProviderCASecretName is set.
func TestOIDCWithProviderCASecret(t *testing.T) {
	tc := OIDCTestEnv
	g := NewWithT(t)

	ensureOIDCClientSecret(t, tc)
	defer DeleteGatewayConfig(t, tc.Ctx, tc.K8sClient)

	// Create provider CA secret (idempotent)
	caSecretName := "oidc-provider-ca"
	caSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      caSecretName,
			Namespace: gateway.GatewayNamespace,
		},
		Data: map[string][]byte{
			"ca.crt": []byte("-----BEGIN CERTIFICATE-----\ntest-ca\n-----END CERTIFICATE-----"),
		},
	}
	if err := tc.K8sClient.Create(tc.Ctx, caSecret); err != nil && !k8serr.IsAlreadyExists(err) {
		t.Fatalf("Failed to create provider CA secret: %v", err)
	}

	CreateGatewayConfig(t, tc.Ctx, tc.K8sClient, oidcSpecWithProviderCA(caSecretName))

	// Wait for Deployment with provider CA volume (controller must update it)
	g.Eventually(func() bool {
		deployment := &appsv1.Deployment{}
		if err := tc.K8sClient.Get(tc.Ctx, types.NamespacedName{
			Name:      gateway.KubeAuthProxyName,
			Namespace: gateway.GatewayNamespace,
		}, deployment); err != nil {
			return false
		}
		for _, vol := range deployment.Spec.Template.Spec.Volumes {
			if vol.Name == "provider-ca-cert" {
				return true
			}
		}
		return false
	}, TestTimeout, TestInterval).Should(BeTrue(), "Deployment should have provider CA volume")

	deployment, err := getAuthProxyDeployment(tc.Ctx, tc.K8sClient)
	g.Expect(err).To(Succeed())

	// Verify provider CA volume exists
	hasCAVolume := false
	for _, vol := range deployment.Spec.Template.Spec.Volumes {
		if vol.Name == "provider-ca-cert" {
			hasCAVolume = true
			g.Expect(vol.Secret).NotTo(BeNil())
			g.Expect(vol.Secret.SecretName).To(Equal(caSecretName))
			break
		}
	}
	g.Expect(hasCAVolume).To(BeTrue(), "Should have provider CA volume")

	// Verify volume mount
	hasCAMount := false
	for _, vm := range deployment.Spec.Template.Spec.Containers[0].VolumeMounts {
		if vm.Name == "provider-ca-cert" {
			hasCAMount = true
			g.Expect(vm.MountPath).To(Equal("/etc/provider-ca"))
			g.Expect(vm.ReadOnly).To(BeTrue())
			break
		}
	}
	g.Expect(hasCAMount).To(BeTrue(), "Should have provider CA volume mount")

	// Verify --provider-ca-file arg
	args := deployment.Spec.Template.Spec.Containers[0].Args
	g.Expect(args).To(ContainElement("--provider-ca-file=/etc/provider-ca/ca.crt"))
}

// TestOIDCWithInsecureSkipVerify validates that VerifyProviderCertificate=false results in --ssl-insecure-skip-verify=true in the deployment.
func TestOIDCWithInsecureSkipVerify(t *testing.T) {
	tc := OIDCTestEnv
	g := NewWithT(t)

	ensureOIDCClientSecret(t, tc)
	defer DeleteGatewayConfig(t, tc.Ctx, tc.K8sClient)

	CreateGatewayConfig(t, tc.Ctx, tc.K8sClient, oidcSpecWithVerifyProviderCertificate(false))

	g.Eventually(func() bool {
		gc := &serviceApi.GatewayConfig{}
		if err := tc.K8sClient.Get(tc.Ctx, types.NamespacedName{Name: serviceApi.GatewayConfigName}, gc); err != nil {
			return false
		}
		if gc.Spec.VerifyProviderCertificate == nil || *gc.Spec.VerifyProviderCertificate {
			return false
		}
		deployment, err := getAuthProxyDeployment(tc.Ctx, tc.K8sClient)
		if err != nil {
			return false
		}
		for _, arg := range deployment.Spec.Template.Spec.Containers[0].Args {
			if arg == "--ssl-insecure-skip-verify=true" {
				return true
			}
		}
		return false
	}, TestTimeout, TestInterval).Should(BeTrue(), "GatewayConfig VerifyProviderCertificate and deployment --ssl-insecure-skip-verify must be updated")
}

// TestOIDCSpecMutationCookieConfig validates cookie spec mutation in OIDC mode (delegates to RunSpecMutationCookieConfigTest).
func TestOIDCSpecMutationCookieConfig(t *testing.T) {
	RunSpecMutationCookieConfigTest(t, GetOIDCTestSetup(), SpecMutationCookieConfig())
}

// TestOIDCSpecMutationIssuerURLChange validates that GatewayConfig IssuerURL and deployment oidc-issuer-url update when issuer URL changes.
func TestOIDCSpecMutationIssuerURLChange(t *testing.T) {
	tc := OIDCTestEnv
	g := NewWithT(t)

	ensureOIDCClientSecret(t, tc)
	defer DeleteGatewayConfig(t, tc.Ctx, tc.K8sClient)

	// Create with initial issuer URL
	CreateGatewayConfig(t, tc.Ctx, tc.K8sClient, getOIDCGatewayConfigSpec())

	// Wait for Deployment with initial issuer URL
	g.Eventually(func() bool {
		deployment := &appsv1.Deployment{}
		if err := tc.K8sClient.Get(tc.Ctx, types.NamespacedName{
			Name:      gateway.KubeAuthProxyName,
			Namespace: gateway.GatewayNamespace,
		}, deployment); err != nil {
			return false
		}
		args := deployment.Spec.Template.Spec.Containers[0].Args
		for _, arg := range args {
			if arg == fmt.Sprintf("--oidc-issuer-url=%s", OIDCIssuerURL) {
				return true
			}
		}
		return false
	}, TestTimeout, TestInterval).Should(BeTrue())

	newIssuerURL := "https://new-keycloak.example.com/realms/new-realm"
	UpdateGatewayConfig(t, tc.Ctx, tc.K8sClient, oidcSpecWithIssuerURL(newIssuerURL))

	g.Eventually(func() bool {
		gc := &serviceApi.GatewayConfig{}
		if err := tc.K8sClient.Get(tc.Ctx, types.NamespacedName{Name: serviceApi.GatewayConfigName}, gc); err != nil {
			return false
		}
		if gc.Spec.OIDC == nil || gc.Spec.OIDC.IssuerURL != newIssuerURL {
			return false
		}
		deployment, err := getAuthProxyDeployment(tc.Ctx, tc.K8sClient)
		if err != nil {
			return false
		}
		for _, arg := range deployment.Spec.Template.Spec.Containers[0].Args {
			if arg == fmt.Sprintf("--oidc-issuer-url=%s", newIssuerURL) {
				return true
			}
		}
		return false
	}, TestTimeout, TestInterval).Should(BeTrue(), "GatewayConfig IssuerURL and deployment oidc-issuer-url must be updated")
}

// TestOIDCGatewayConfigStatusConditions validates GatewayConfig status conditions in OIDC mode (delegates to RunGatewayConfigStatusConditionsTest).
func TestOIDCGatewayConfigStatusConditions(t *testing.T) {
	RunGatewayConfigStatusConditionsTest(t, GetOIDCTestSetup())
}

// TestOIDCGatewayConfigStatusDomain validates GatewayConfig status domain in OIDC mode (delegates to RunGatewayConfigStatusDomainTest).
func TestOIDCGatewayConfigStatusDomain(t *testing.T) {
	RunGatewayConfigStatusDomainTest(t, GetOIDCTestSetup(), DefaultGatewayHost(OIDCClusterDomain))
}

// TestOIDCLoadBalancerIngressMode validates LoadBalancer ingress mode in OIDC (delegates to RunLoadBalancerIngressModeTest).
func TestOIDCLoadBalancerIngressMode(t *testing.T) {
	ensureOIDCClientSecret(t, OIDCTestEnv)
	RunLoadBalancerIngressModeTest(t, OIDCTestEnv, oidcSpecWithLoadBalancer())
}

// TestOIDCSubdomainChange validates that GatewayConfig subdomain and route host update when subdomain changes.
func TestOIDCSubdomainChange(t *testing.T) {
	tc := OIDCTestEnv
	g := NewWithT(t)

	ensureOIDCClientSecret(t, tc)
	defer DeleteGatewayConfig(t, tc.Ctx, tc.K8sClient)

	CreateGatewayConfig(t, tc.Ctx, tc.K8sClient, oidcSpecWithSubdomain("custom-oidc-gateway"))

	g.Eventually(func() bool {
		gc := &serviceApi.GatewayConfig{}
		if err := tc.K8sClient.Get(tc.Ctx, types.NamespacedName{Name: serviceApi.GatewayConfigName}, gc); err != nil {
			return false
		}
		if gc.Spec.Subdomain != "custom-oidc-gateway" {
			return false
		}
		route := &routev1.Route{}
		if err := tc.K8sClient.Get(tc.Ctx, types.NamespacedName{
			Name:      gateway.DefaultGatewayName,
			Namespace: gateway.GatewayNamespace,
		}, route); err != nil {
			return false
		}
		return route.Spec.Host == HostForSubdomain("custom-oidc-gateway", OIDCClusterDomain)
	}, TestTimeout, TestInterval).Should(BeTrue(), "GatewayConfig subdomain and route host must be updated")
}

// TestOIDCSecretNamespace validates that kube-auth-proxy-creds is populated from OIDC client secret in a custom namespace.
func TestOIDCSecretNamespace(t *testing.T) {
	tc := OIDCTestEnv
	g := NewWithT(t)

	// Create a custom namespace for the secret
	customNamespace := "custom-oidc-secrets"
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: customNamespace,
		},
	}
	if err := tc.K8sClient.Create(tc.Ctx, ns); err != nil && !k8serr.IsAlreadyExists(err) {
		t.Fatalf("Failed to create custom namespace: %v", err)
	}

	// Create OIDC client secret in custom namespace
	customSecretName := "custom-oidc-secret"
	customSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      customSecretName,
			Namespace: customNamespace,
		},
		Data: map[string][]byte{
			OIDCSecretKey: []byte("custom-client-secret"),
		},
	}
	if err := tc.K8sClient.Create(tc.Ctx, customSecret); err != nil && !k8serr.IsAlreadyExists(err) {
		t.Fatalf("Failed to create custom OIDC secret: %v", err)
	}

	defer DeleteGatewayConfig(t, tc.Ctx, tc.K8sClient)

	// Create GatewayConfig with custom secret namespace
	spec := serviceApi.GatewayConfigSpec{
		IngressMode: serviceApi.IngressModeOcpRoute,
		OIDC: &serviceApi.OIDCConfig{
			IssuerURL: OIDCIssuerURL,
			ClientID:  OIDCClientID,
			ClientSecretRef: corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: customSecretName,
				},
				Key: OIDCSecretKey,
			},
			SecretNamespace: customNamespace,
		},
	}
	CreateGatewayConfig(t, tc.Ctx, tc.K8sClient, spec)

	// Wait for kube-auth-proxy-creds secret to be created (indicates controller processed the custom namespace)
	g.Eventually(func() error {
		secret := &corev1.Secret{}
		return tc.K8sClient.Get(tc.Ctx, types.NamespacedName{
			Name:      gateway.KubeAuthProxySecretsName,
			Namespace: gateway.GatewayNamespace,
		}, secret)
	}, TestTimeout, TestInterval).Should(Succeed())

	// Verify the secret has the client secret from custom namespace
	secret := &corev1.Secret{}
	g.Expect(tc.K8sClient.Get(tc.Ctx, types.NamespacedName{
		Name:      gateway.KubeAuthProxySecretsName,
		Namespace: gateway.GatewayNamespace,
	}, secret)).To(Succeed())
	g.Expect(secret.Data).To(HaveKey(gateway.EnvClientSecret))
}

// TestOIDCMissingConfigSetsReadyFalse verifies Ready=False when OIDC is required but not configured.
// Keep this test last in the OIDC test list: it deletes GatewayConfig at the end; a test running after could race with the controller.
func TestOIDCMissingConfigSetsReadyFalse(t *testing.T) {
	tc := OIDCTestEnv
	g := NewWithT(t)
	defer DeleteGatewayConfig(t, tc.Ctx, tc.K8sClient)

	// Create GatewayConfig WITHOUT OIDC configuration (but cluster is in OIDC mode)
	CreateGatewayConfig(t, tc.Ctx, tc.K8sClient, serviceApi.GatewayConfigSpec{
		IngressMode: serviceApi.IngressModeOcpRoute,
	})

	// Verify GatewayConfig has Ready=False condition
	g.Eventually(func() bool {
		gc := &serviceApi.GatewayConfig{}
		if err := tc.K8sClient.Get(tc.Ctx, types.NamespacedName{Name: serviceApi.GatewayConfigName}, gc); err != nil {
			return false
		}
		for _, cond := range gc.Status.Conditions {
			if cond.Type == "Ready" && cond.Status == metav1.ConditionFalse {
				return true
			}
		}
		return false
	}, TestTimeout, TestInterval).Should(BeTrue())
}
