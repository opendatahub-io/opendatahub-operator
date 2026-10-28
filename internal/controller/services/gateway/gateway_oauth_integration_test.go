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
	"strconv"
	"strings"
	"testing"
	"time"

	oauthv1 "github.com/openshift/api/oauth/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"

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

// TestOAuthAdditionalGateways validates per-ingress Gateway reconciliation in OAuth mode.
func TestOAuthAdditionalGateways(t *testing.T) {
	RunAdditionalGatewaysTest(t, GetOAuthTestSetup())
}

// TestOAuthAdditionalIngressRouteLabelsUpdate validates routeLabels updates in OAuth mode.
func TestOAuthAdditionalIngressRouteLabelsUpdate(t *testing.T) {
	RunAdditionalIngressRouteLabelsUpdateTest(t, GetOAuthTestSetup())
}

// TestOAuthAdditionalIngressReorder validates that reordering additionalIngresses does not churn resources.
func TestOAuthAdditionalIngressReorder(t *testing.T) {
	RunAdditionalIngressReorderTest(t, GetOAuthTestSetup())
}

// TestOAuthAdditionalIngressIdempotency validates that repeated reconciliation leaves resources untouched.
func TestOAuthAdditionalIngressIdempotency(t *testing.T) {
	RunAdditionalIngressIdempotencyTest(t, GetOAuthTestSetup())
}

func additionalOAuthIngressResources(name string) []client.Object {
	proxyName := gateway.KubeAuthProxyName + "-" + name
	objects := make([]client.Object, 0, 14)
	for kind, resourceName := range map[schema.GroupVersionKind]string{
		gvk.Deployment:              proxyName,
		gvk.Service:                 proxyName,
		gvk.HorizontalPodAutoscaler: proxyName,
		gvk.ServiceAccount:          proxyName,
		gvk.NetworkPolicy:           proxyName,
		gvk.Secret:                  name + "-auth-creds",
		gvk.ClusterRoleBinding:      proxyName + "-tokenreview",
		gvk.OAuthClient:             gateway.AuthClientID + "-" + name,
		gvk.HTTPRoute:               gateway.OAuthCallbackRouteName + "-" + name,
		gvk.EnvoyFilter:             name + "-authn",
		gvk.DestinationRule:         name + "-auth-destination",
		gvk.KubernetesGateway:       name,
		gvk.Route:                   name,
		gvk.ConfigMap:               name + "-config",
	} {
		obj := resources.GvkToUnstructured(kind)
		obj.SetName(resourceName)
		if kind != gvk.ClusterRoleBinding && kind != gvk.OAuthClient {
			obj.SetNamespace(gateway.GetGatewayNamespace())
		}
		objects = append(objects, obj)
	}
	return objects
}

func TestOAuthAdditionalAuthenticationResources(t *testing.T) {
	setup := GetOAuthTestSetup()
	defer setup.Setup(t)()
	ctx, cli := setup.TC.Ctx, setup.TC.K8sClient
	g := NewWithT(t)
	spec := setup.Spec
	spec.AdditionalIngresses = serviceApi.AdditionalIngresses{{
		Name:                  "auth-check",
		Hostname:              "auth-check.example.com",
		IngressControllerName: "shard-auth-check",
		RouteLabels:           map[string]string{"example.com/ingress": "auth-check"},
	}, {
		Name: "remove-check", Hostname: "remove-check.example.com", IngressControllerName: "shard-remove-check",
		RouteLabels: map[string]string{"example.com/ingress": "remove-check"},
	}}
	UpdateGatewayConfig(t, ctx, cli, spec)
	for _, name := range []string{"auth-check", "remove-check"} {
		for _, obj := range additionalOAuthIngressResources(name) {
			key := client.ObjectKeyFromObject(obj)
			g.Eventually(func(g Gomega) {
				g.Expect(cli.Get(ctx, key, obj)).To(Succeed())
				g.Expect(obj.GetAnnotations()["platform.opendatahub.io/instance.generation"]).NotTo(BeEmpty(), "%T %s needs deployment metadata", obj, key)
			}, TestTimeout, TestInterval).Should(Succeed())
		}
	}
	key := types.NamespacedName{
		Name:      "kube-auth-proxy-auth-check",
		Namespace: gateway.GetGatewayNamespace(),
	}
	callback := &gwapiv1.HTTPRoute{}
	g.Eventually(func() error {
		return cli.Get(ctx,
			types.NamespacedName{
				Name:      gateway.OAuthCallbackRouteName + "-auth-check",
				Namespace: key.Namespace,
			}, callback)
	}, TestTimeout, TestInterval).Should(Succeed())
	g.Expect(callback.Spec.ParentRefs).To(HaveLen(1))
	g.Expect(string(callback.Spec.ParentRefs[0].Name)).To(Equal("auth-check"))
	g.Expect(string(callback.Spec.Rules[0].BackendRefs[0].Name)).To(Equal(key.Name))
	filter := resources.GvkToUnstructured(gvk.EnvoyFilter)
	g.Eventually(func() error {
		return cli.Get(ctx,
			types.NamespacedName{
				Name:      "auth-check-authn",
				Namespace: key.Namespace,
			}, filter)
	}, TestTimeout, TestInterval).Should(Succeed())
	selector, found, err := unstructured.NestedStringMap(filter.Object, "spec", "workloadSelector", "labels")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(selector["gateway.networking.k8s.io/gateway-name"]).To(Equal("auth-check"))

	// Owned auth dependencies must be recreated after deletion, without a spec change.
	for _, obj := range []client.Object{&corev1.ServiceAccount{}, &networkingv1.NetworkPolicy{}} {
		g.Expect(cli.Get(ctx, key, obj)).To(Succeed())
		oldUID := obj.GetUID()
		g.Expect(cli.Delete(ctx, obj)).To(Succeed())
		g.Eventually(func() error {
			if err := cli.Get(ctx, key, obj); err != nil {
				return err
			}
			if obj.GetUID() == oldUID {
				return fmt.Errorf("%T has not been recreated", obj)
			}
			return nil
		}, TestTimeout, TestInterval).Should(Succeed())
	}

	// A preparation failure after a generation change must preserve the existing stack.
	deployment := &appsv1.Deployment{}
	g.Expect(cli.Get(ctx, key, deployment)).To(Succeed())
	oldUID := deployment.UID
	credentials := &corev1.Secret{}
	credentialsKey := types.NamespacedName{
		Name:      "auth-check-auth-creds",
		Namespace: key.Namespace,
	}
	g.Expect(cli.Get(ctx, credentialsKey, credentials)).To(Succeed())
	owners := credentials.OwnerReferences
	credentials.OwnerReferences = append([]metav1.OwnerReference(nil), owners...)
	credentials.OwnerReferences[0].Name = "another-gateway-config"
	credentials.OwnerReferences[0].UID = "another-gateway-config"
	g.Expect(cli.Update(ctx, credentials)).To(Succeed())
	spec.AdditionalIngresses[0].Hostname = "updated-auth-check.example.com"
	spec.AdditionalIngresses = spec.AdditionalIngresses[:1]
	UpdateGatewayConfig(t, ctx, cli, spec)
	g.Eventually(func(g Gomega) {
		config, err := getGatewayConfig(ctx, cli)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(config.Status.AdditionalIngresses).To(HaveLen(1))
		condition := conditions.FindStatusCondition(&config.Status.AdditionalIngresses[0], serviceApi.AdditionalIngressAuthenticationReadyConditionType)
		g.Expect(condition).NotTo(BeNil())
		g.Expect(condition.ObservedGeneration).To(Equal(config.Generation))
		g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
	}, TestTimeout, TestInterval).Should(Succeed())
	waitForGatewayResourcesRemoved(t, setup.TC, additionalOAuthIngressResources("remove-check")...)
	g.Consistently(func(g Gomega) {
		g.Expect(cli.Get(ctx, key, deployment)).To(Succeed())
		g.Expect(deployment.UID).To(Equal(oldUID))
		g.Expect(deployment.DeletionTimestamp).To(BeNil())
	}, 5*time.Second, TestInterval).Should(Succeed(), "GC must retain resources when auth preparation is incomplete")

	// Fixing preparation must resume normal deployment and subsequent removal cleanup.
	g.Expect(cli.Get(ctx, credentialsKey, credentials)).To(Succeed())
	credentials.OwnerReferences = owners
	g.Expect(cli.Update(ctx, credentials)).To(Succeed())
	spec.AdditionalIngresses[0].Hostname = "auth-check.example.com"
	UpdateGatewayConfig(t, ctx, cli, spec)
	g.Eventually(func(g Gomega) {
		config, err := getGatewayConfig(ctx, cli)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(cli.Get(ctx, key, deployment)).To(Succeed())
		g.Expect(deployment.Annotations).To(HaveKeyWithValue("platform.opendatahub.io/instance.generation", strconv.FormatInt(config.Generation, 10)))
	}, TestTimeout, TestInterval).Should(Succeed())
	g.Expect(deployment.UID).To(Equal(oldUID))

	spec.AdditionalIngresses = nil
	UpdateGatewayConfig(t, ctx, cli, spec)
	waitForGatewayResourcesRemoved(t, setup.TC, additionalOAuthIngressResources("auth-check")...)
	g.Expect(cli.Get(ctx, types.NamespacedName{Name: gateway.KubeAuthProxySecretsName, Namespace: key.Namespace}, &corev1.Secret{})).To(Succeed())
	g.Expect(cli.Get(ctx, types.NamespacedName{Name: gateway.AuthClientID}, &oauthv1.OAuthClient{})).To(Succeed())
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
		[]string{"--provider=openshift", "--scope=user:full", "--ssl-insecure-skip-verify=false", "--pass-access-token=true"},
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

func TestOAuthNetworkPolicyReconciliation(t *testing.T) {
	RunNetworkPolicyReconciliationTest(t, GetOAuthTestSetup())
}

func TestOAuthNetworkPolicyAuthModeLifecycle(t *testing.T) {
	RunNetworkPolicyAuthModeLifecycleTest(t, GetOAuthTestSetup())
}

// TestOAuthNginxDashboardRedirectSkippedWithoutDashboard validates redirects are skipped when Dashboard is not deployed in OAuth mode.
func TestOAuthNginxDashboardRedirectSkippedWithoutDashboard(t *testing.T) {
	RunNginxDashboardRedirectSkippedWithoutDashboardTest(t, GetOAuthTestSetup())
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

// TestOAuthSpecMutationTokenReviewConfig validates token review spec mutation in OAuth mode.
func TestOAuthSpecMutationTokenReviewConfig(t *testing.T) {
	RunSpecMutationTokenReviewConfigTest(t, GetOAuthTestSetup(), SpecMutationTokenReviewConfig())
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
