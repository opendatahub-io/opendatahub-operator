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

const (
	oidcClusterDomain = "apps.oidc-test.example.com"
	oidcIssuerURL     = "https://keycloak.example.com/realms/test"
	oidcClientID      = "test-oidc-client"
	oidcSecretName    = "oidc-client-secret"
	oidcSecretKey     = "client-secret"
)

// ensureOIDCClientSecret creates the OIDC client secret if it doesn't exist.
// This is idempotent for use with shared test environments.
func ensureOIDCClientSecret(t *testing.T, tc *TestEnvContext) {
	t.Helper()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      oidcSecretName,
			Namespace: gateway.GatewayNamespace,
		},
		Data: map[string][]byte{
			oidcSecretKey: []byte("test-client-secret"),
		},
	}
	if err := tc.K8sClient.Create(tc.Ctx, secret); err != nil && !k8serr.IsAlreadyExists(err) {
		t.Fatalf("Failed to create OIDC client secret: %v", err)
	}
}

// getOIDCGatewayConfigSpec returns the standard OIDC GatewayConfig spec.
func getOIDCGatewayConfigSpec() serviceApi.GatewayConfigSpec {
	return serviceApi.GatewayConfigSpec{
		IngressMode: serviceApi.IngressModeOcpRoute,
		OIDC: &serviceApi.OIDCConfig{
			IssuerURL: oidcIssuerURL,
			ClientID:  oidcClientID,
			ClientSecretRef: corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: oidcSecretName,
				},
				Key: oidcSecretKey,
			},
		},
	}
}

// GetOIDCTestSetup returns the test setup for OIDC mode.
func GetOIDCTestSetup() TestSetup {
	return TestSetup{
		TC:        OIDCTestEnv,
		Spec:      getOIDCGatewayConfigSpec(),
		SetupFunc: ensureOIDCClientSecret,
	}
}

// GetOIDCTestSetupForExtAuthz returns setup with AuthProxyTimeout for ext_authz tests.
func GetOIDCTestSetupForExtAuthz() TestSetup {
	spec := getOIDCGatewayConfigSpec()
	spec.AuthProxyTimeout = metav1.Duration{Duration: 45 * time.Second}
	return TestSetup{
		TC:        OIDCTestEnv,
		Spec:      spec,
		SetupFunc: ensureOIDCClientSecret,
	}
}

func TestOIDCGatewayClassCreation(t *testing.T) {
	RunGatewayClassCreationTest(t, GetOIDCTestSetup())
}

func TestOIDCGatewayCreation(t *testing.T) {
	RunGatewayCreationTest(t, GetOIDCTestSetup())
}

func TestOIDCHTTPRouteCreation(t *testing.T) {
	RunHTTPRouteCreationTest(t, GetOIDCTestSetup())
}

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

func TestOIDCAuthProxySecretCreation(t *testing.T) {
	RunAuthProxySecretCreationTest(t, GetOIDCTestSetup(), oidcClientID)
}

func TestOIDCServiceCreation(t *testing.T) {
	RunServiceCreationTest(t, GetOIDCTestSetup())
}

func TestOIDCDeploymentWithAllArgs(t *testing.T) {
	expectedHostname := gateway.DefaultGatewaySubdomain + "." + oidcClusterDomain
	RunDeploymentWithAllArgsTest(t, GetOIDCTestSetup(), expectedHostname,
		[]string{"--provider=oidc", fmt.Sprintf("--oidc-issuer-url=%s", oidcIssuerURL), "--skip-oidc-discovery=false", "--ssl-insecure-skip-verify=false"},
		[]string{"--provider=openshift", "--scope=user:full"})
}

func TestOIDCHPACreation(t *testing.T) {
	RunHPACreationTest(t, GetOIDCTestSetup())
}

func TestOIDCEnvoyFilterCreation(t *testing.T) {
	RunEnvoyFilterCreationTest(t, GetOIDCTestSetup())
}

func TestOIDCEnvoyFilterExtAuthzConfiguration(t *testing.T) {
	RunEnvoyFilterExtAuthzConfigurationTest(t, GetOIDCTestSetupForExtAuthz())
}

func TestOIDCEnvoyFilterOrder(t *testing.T) {
	RunEnvoyFilterOrderTest(t, GetOIDCTestSetup())
}

func TestOIDCEnvoyFilterLuaTokenForwarding(t *testing.T) {
	RunEnvoyFilterLuaTokenForwardingTest(t, GetOIDCTestSetup())
}

func TestOIDCEnvoyFilterLegacyRedirectPresent(t *testing.T) {
	RunEnvoyFilterLegacyRedirectPresentTest(t, GetOIDCTestSetup())
}

func TestOIDCEnvoyFilterLegacyRedirectOrderFirst(t *testing.T) {
	RunEnvoyFilterLegacyRedirectOrderFirstTest(t, GetOIDCTestSetup())
}

func TestOIDCDestinationRuleCreation(t *testing.T) {
	RunDestinationRuleCreationTest(t, GetOIDCTestSetup())
}

func TestOIDCOCPRouteCreation(t *testing.T) {
	RunOCPRouteCreationTest(t, GetOIDCTestSetup(), gateway.DefaultGatewaySubdomain+"."+oidcClusterDomain)
}

func TestOIDCLegacyRedirectRouteCreation(t *testing.T) {
	RunLegacyRedirectRouteCreationTest(t, GetOIDCTestSetup(), gateway.LegacyGatewaySubdomain+"."+oidcClusterDomain)
}

func TestOIDCLegacyRouteRemovedWhenSubdomainChangesToLegacy(t *testing.T) {
	// Skip: GC (garbage collection) behavior doesn't work reliably in envtest.
	// The GC action runs but doesn't delete resources because envtest lacks full
	// RBAC/discovery capabilities that the GC relies on. This behavior works correctly
	// in production - verified manually.
	// TODO: Consider E2E test for GC behavior verification.
	t.Skip("Skipping: GC behavior not reliable in envtest - works in production")

	RunLegacyRouteRemovedWhenSubdomainChangesToLegacyTest(t, GetOIDCTestSetup())
}

func TestOIDCNetworkPolicyCreation(t *testing.T) {
	RunNetworkPolicyCreationTest(t, GetOIDCTestSetup())
}

func TestOIDCNetworkPolicyDisabled(t *testing.T) {
	ensureOIDCClientSecret(t, OIDCTestEnv)
	spec := getOIDCGatewayConfigSpec()
	spec.NetworkPolicy = &serviceApi.NetworkPolicyConfig{
		Ingress: &serviceApi.IngressPolicyConfig{Enabled: false},
	}
	RunNetworkPolicyDisabledTest(t, GetOIDCTestSetup(), spec)
}

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

	spec := getOIDCGatewayConfigSpec()
	spec.ProviderCASecretName = caSecretName
	CreateGatewayConfig(t, tc.Ctx, tc.K8sClient, spec)

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

	deployment := &appsv1.Deployment{}
	g.Expect(tc.K8sClient.Get(tc.Ctx, types.NamespacedName{
		Name:      gateway.KubeAuthProxyName,
		Namespace: gateway.GatewayNamespace,
	}, deployment)).To(Succeed())

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

func TestOIDCWithInsecureSkipVerify(t *testing.T) {
	tc := OIDCTestEnv
	g := NewWithT(t)

	ensureOIDCClientSecret(t, tc)
	defer DeleteGatewayConfig(t, tc.Ctx, tc.K8sClient)

	verifyFalse := false
	spec := getOIDCGatewayConfigSpec()
	spec.VerifyProviderCertificate = &verifyFalse
	CreateGatewayConfig(t, tc.Ctx, tc.K8sClient, spec)

	// Wait for Deployment with --ssl-insecure-skip-verify=true arg (controller must update it)
	g.Eventually(func() bool {
		deployment := &appsv1.Deployment{}
		if err := tc.K8sClient.Get(tc.Ctx, types.NamespacedName{
			Name:      gateway.KubeAuthProxyName,
			Namespace: gateway.GatewayNamespace,
		}, deployment); err != nil {
			return false
		}
		for _, arg := range deployment.Spec.Template.Spec.Containers[0].Args {
			if arg == "--ssl-insecure-skip-verify=true" {
				return true
			}
		}
		return false
	}, TestTimeout, TestInterval).Should(BeTrue(), "Deployment should have --ssl-insecure-skip-verify=true arg")

	deployment := &appsv1.Deployment{}
	g.Expect(tc.K8sClient.Get(tc.Ctx, types.NamespacedName{
		Name:      gateway.KubeAuthProxyName,
		Namespace: gateway.GatewayNamespace,
	}, deployment)).To(Succeed())

	args := deployment.Spec.Template.Spec.Containers[0].Args
	g.Expect(args).To(ContainElement("--ssl-insecure-skip-verify=true"))
}

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

func TestOIDCSpecMutationCookieConfig(t *testing.T) {
	RunSpecMutationCookieConfigTest(t, GetOIDCTestSetup(), serviceApi.CookieConfig{
		Expire:  metav1.Duration{Duration: 48 * time.Hour},
		Refresh: metav1.Duration{Duration: 2 * time.Hour},
	})
}

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
			if arg == fmt.Sprintf("--oidc-issuer-url=%s", oidcIssuerURL) {
				return true
			}
		}
		return false
	}, TestTimeout, TestInterval).Should(BeTrue())

	// Update issuer URL
	newIssuerURL := "https://new-keycloak.example.com/realms/new-realm"
	spec := getOIDCGatewayConfigSpec()
	spec.OIDC.IssuerURL = newIssuerURL
	UpdateGatewayConfig(t, tc.Ctx, tc.K8sClient, spec)

	// Verify Deployment args are updated
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
			if arg == fmt.Sprintf("--oidc-issuer-url=%s", newIssuerURL) {
				return true
			}
		}
		return false
	}, TestTimeout, TestInterval).Should(BeTrue())
}

func TestOIDCGatewayConfigStatusConditions(t *testing.T) {
	RunGatewayConfigStatusConditionsTest(t, GetOIDCTestSetup())
}

func TestOIDCGatewayConfigStatusDomain(t *testing.T) {
	RunGatewayConfigStatusDomainTest(t, GetOIDCTestSetup(), gateway.DefaultGatewaySubdomain+"."+oidcClusterDomain)
}

func TestOIDCSubdomainChange(t *testing.T) {
	tc := OIDCTestEnv
	g := NewWithT(t)

	ensureOIDCClientSecret(t, tc)
	defer DeleteGatewayConfig(t, tc.Ctx, tc.K8sClient)

	// Create with custom subdomain
	spec := getOIDCGatewayConfigSpec()
	spec.Subdomain = "custom-oidc-gateway"
	CreateGatewayConfig(t, tc.Ctx, tc.K8sClient, spec)

	customHostname := "custom-oidc-gateway." + oidcClusterDomain

	// Wait for Route with custom subdomain
	g.Eventually(func() bool {
		route := &routev1.Route{}
		if err := tc.K8sClient.Get(tc.Ctx, types.NamespacedName{
			Name:      gateway.DefaultGatewayName,
			Namespace: gateway.GatewayNamespace,
		}, route); err != nil {
			return false
		}
		return route.Spec.Host == customHostname
	}, TestTimeout, TestInterval).Should(BeTrue(), "Route should have custom subdomain hostname")
}

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
			oidcSecretKey: []byte("custom-client-secret"),
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
			IssuerURL: oidcIssuerURL,
			ClientID:  oidcClientID,
			ClientSecretRef: corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: customSecretName,
				},
				Key: oidcSecretKey,
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
