//go:build !integration

//nolint:testpackage
package gateway

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/stretchr/testify/mock"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/validate/content"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	conditionstest "github.com/opendatahub-io/opendatahub-operator/v2/internal/testutil/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	actionerrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/mocks"

	. "github.com/onsi/gomega"
)

func newGatewayAuthTestConfig(ingresses ...serviceApi.AdditionalIngress) *serviceApi.GatewayConfig {
	return &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName, UID: "config", Generation: 1},
		Spec: serviceApi.GatewayConfigSpec{
			Domain:              "apps.example.com",
			IngressMode:         serviceApi.IngressModeOcpRoute,
			AdditionalIngresses: ingresses,
		},
	}
}

func newGatewayAuthTestRequest(t *testing.T, config *serviceApi.GatewayConfig, objects ...client.Object) *odhtypes.ReconciliationRequest {
	t.Helper()
	return &odhtypes.ReconciliationRequest{
		Client:     newGatewayTestClient(t, append([]client.Object{config}, objects...)...),
		Instance:   config,
		Conditions: conditionstest.NewManager(config, ReadyConditionType, serviceApi.AdditionalGatewaysReadyConditionType),
	}
}

func TestAdditionalAuthNamesFitMaximumIngressName(t *testing.T) {
	name := strings.Repeat("a", serviceApi.MaxAdditionalGatewayNameLength)
	target := additionalAuthProxyTarget(serviceApi.AdditionalIngress{
		Name: name,
	}, &serviceApi.GatewayConfig{})
	for _, resourceName := range []string{target.ProxyName, target.SecretName, target.TLSName, target.ClientID,
		target.CallbackName, target.FilterName, target.DestinationRuleName} {
		if problems := validation.IsDNS1123Subdomain(resourceName); len(problems) != 0 || len(resourceName) > 63 {
			t.Errorf("invalid resource name %q: %v", resourceName, problems)
		}
	}
	// RBAC names are path segments, so the proxy-name suffix need not fit a DNS label.
	if problems := content.IsPathSegmentName(target.ProxyName + "-tokenreview"); len(problems) != 0 {
		t.Errorf("invalid ClusterRoleBinding name: %v", problems)
	}
}

func TestAdditionalIngressWithoutAuthentication(t *testing.T) {
	g := NewWithT(t)
	original := cluster.GetClusterInfo()
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
	t.Cleanup(func() { cluster.SetClusterInfo(original) })
	ingress := additionalIngress("alpha", "alpha.example.com", "alpha")
	config := newGatewayAuthTestConfig(ingress)
	authentication := &configv1.Authentication{
		ObjectMeta: metav1.ObjectMeta{
			Name: cluster.ClusterAuthenticationObj,
		},
		Spec: configv1.AuthenticationSpec{
			Type: configv1.AuthenticationTypeNone,
		},
	}
	rr := newGatewayAuthTestRequest(t, config, authentication)
	g.Expect(syncAdditionalIngressStatus(t.Context(), rr)).To(Succeed())
	g.Expect(createKubeAuthProxyInfrastructure(t.Context(), rr)).To(Succeed())
	g.Expect(rr.Resources).To(BeEmpty(), "None must not render an auth proxy or filter")
	g.Expect(rr.Templates).To(BeEmpty())
	g.Expect(rr.Client.Get(t.Context(), types.NamespacedName{
		Name:      "alpha-auth-creds",
		Namespace: GetGatewayNamespace(),
	}, &corev1.Secret{})).To(MatchError(ContainSubstring("not found")))
	status := additionalIngressStatusByName(config, ingress.Name)
	g.Expect(syncAdditionalIngressAuthReadiness(t.Context(), rr, config, ingress, status)).To(BeFalse())
	condition := conditions.FindStatusCondition(status, serviceApi.AdditionalIngressAuthenticationReadyConditionType)
	g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
	g.Expect(condition.Reason).To(Equal("AuthenticationDisabled"))
	g.Expect(createAdditionalIngressRoutes(t.Context(), rr, config)).To(Succeed())
	g.Expect(rr.Resources).To(HaveLen(1))
	g.Expect(rr.Resources[0].GetKind()).To(Equal(gvk.Route.Kind))
}

func TestMissingOIDCConfigKeepsAuthenticationFilter(t *testing.T) {
	g := NewWithT(t)
	original := cluster.GetClusterInfo()
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
	t.Cleanup(func() { cluster.SetClusterInfo(original) })
	ingress := additionalIngress("alpha", "alpha.example.com", "alpha")
	config := newGatewayAuthTestConfig(ingress)
	authentication := &configv1.Authentication{
		ObjectMeta: metav1.ObjectMeta{
			Name: cluster.ClusterAuthenticationObj,
		},
		Spec: configv1.AuthenticationSpec{
			Type: "OIDC",
		},
	}
	rr := newGatewayAuthTestRequest(t, config, authentication)
	g.Expect(syncAdditionalIngressStatus(t.Context(), rr)).To(Succeed())
	g.Expect(createKubeAuthProxyInfrastructure(t.Context(), rr)).To(MatchError(ContainSubstring("requeue after")))
	g.Expect(rr.Resources).To(HaveLen(1))
	g.Expect(rr.Resources[0].GetKind()).To(Equal(gvk.EnvoyFilter.Kind))
	g.Expect(rr.Templates).To(BeEmpty())
	g.Expect(createAdditionalIngressRoutes(t.Context(), rr, config)).To(Succeed())
	g.Expect(rr.Resources).To(HaveLen(2))
	g.Expect(conditions.FindStatusCondition(additionalIngressStatusByName(config, "alpha"),
		serviceApi.AdditionalIngressAuthenticationReadyConditionType).Reason).To(Equal(additionalIngressReasonInvalidAuthenticationConfig))
}

func TestForeignAuthenticationCredentialsKeepFilterAndBridgeRoute(t *testing.T) {
	g := NewWithT(t)
	original := cluster.GetClusterInfo()
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
	t.Cleanup(func() { cluster.SetClusterInfo(original) })
	ingress := additionalIngress("alpha", "alpha.example.com", "alpha")
	config := newGatewayAuthTestConfig(ingress)
	foreign := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "alpha-auth-creds",
			Namespace: GetGatewayNamespace(),
		},
	}
	rr := newGatewayAuthTestRequest(t, config, foreign)
	g.Expect(syncAdditionalIngressStatus(t.Context(), rr)).To(Succeed())
	g.Expect(prepareAuthProxies(t.Context(), rr, config, cluster.AuthModeIntegratedOAuth,
		kubeAuthProxyDeploymentOauthTemplate, []authProxyTarget{additionalAuthProxyTarget(ingress, config)})).To(MatchError(ContainSubstring("requeue after")))
	g.Expect(rr.Resources).To(HaveLen(1))
	g.Expect(rr.Resources[0].GetKind()).To(Equal(gvk.EnvoyFilter.Kind))
	g.Expect(createAdditionalIngressRoutes(t.Context(), rr, config)).To(Succeed())
	g.Expect(rr.Resources).To(HaveLen(2))
	g.Expect(conditions.FindStatusCondition(additionalIngressStatusByName(config, "alpha"),
		serviceApi.AdditionalIngressAuthenticationReadyConditionType).Reason).To(Equal(additionalIngressReasonOwnershipConflict))
}

func TestOIDCPreparationFailureDoesNotBlockOtherGateways(t *testing.T) {
	original := cluster.GetClusterInfo()
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
	t.Cleanup(func() { cluster.SetClusterInfo(original) })
	for _, failure := range []string{"default-source", "alpha-source", "alpha-config"} {
		t.Run(failure, func(t *testing.T) {
			g := NewWithT(t)
			config := newGatewayAuthTestConfig(
				additionalIngress("alpha", "alpha.example.com", "alpha"),
				additionalIngress("beta", "beta.example.com", "beta"),
			)
			objects := []client.Object{&configv1.Authentication{
				ObjectMeta: metav1.ObjectMeta{Name: cluster.ClusterAuthenticationObj},
				Spec:       configv1.AuthenticationSpec{Type: "OIDC"},
			}}
			for i, name := range []string{"default", "alpha", "beta"} {
				ref := corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: name + "-source"}, Key: "clientSecret",
				}
				if i == 0 {
					config.Spec.OIDC = &serviceApi.OIDCConfig{IssuerURL: "https://issuer.example.com", ClientID: name, ClientSecretRef: ref}
				} else if name+"-config" != failure {
					config.Spec.AdditionalIngresses[i-1].Auth.OIDC = &serviceApi.AdditionalIngressOIDCConfig{ClientID: name, ClientSecretRef: ref}
				}
				if ref.Name != failure {
					objects = append(objects, &corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{Name: ref.Name, Namespace: GetGatewayNamespace()},
						Data:       map[string][]byte{"clientSecret": []byte(name)},
					})
				}
			}
			rr := newGatewayAuthTestRequest(t, config, objects...)
			markGatewayNotReady(rr, "Gateway was previously missing")
			g.Expect(syncAdditionalIngressStatus(t.Context(), rr)).To(Succeed())
			g.Expect(createKubeAuthProxyInfrastructure(t.Context(), rr)).To(MatchError(ContainSubstring("requeue after")))
			g.Expect(rr.Resources).To(ContainElement(WithTransform(func(obj unstructured.Unstructured) string {
				return obj.GetName()
			}, Equal("kube-auth-proxy-beta"))))
			if failure == "default-source" {
				g.Expect(rr.Conditions.GetCondition(ReadyConditionType).Status).To(Equal(metav1.ConditionFalse))
				g.Expect(rr.Conditions.GetCondition(ReadyConditionType).Message).To(ContainSubstring("default-source"))
			} else {
				condition := conditions.FindStatusCondition(additionalIngressStatusByName(config, "alpha"), serviceApi.AdditionalIngressAuthenticationReadyConditionType)
				g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(condition.Message).To(ContainSubstring("alpha"))
			}
		})
	}
}

func TestAuthenticationCleanupProtectsOnlyFailedTargets(t *testing.T) {
	g := NewWithT(t)
	config := &serviceApi.GatewayConfig{Spec: serviceApi.GatewayConfigSpec{
		AdditionalIngresses: serviceApi.AdditionalIngresses{additionalIngress("alpha", "alpha.example.com", "alpha")},
	}}
	rr := &odhtypes.ReconciliationRequest{Instance: config}
	gatewayAuthState(rr)["alpha"] = additionalAuthProxyTarget(config.Spec.AdditionalIngresses[0], config)
	for kind, name := range map[schema.GroupVersionKind]string{
		gvk.Secret: "alpha-auth-creds", gvk.Deployment: "kube-auth-proxy-alpha",
		gvk.ClusterRoleBinding: "kube-auth-proxy-alpha-tokenreview", gvk.OAuthClient: AuthClientID + "-alpha", gvk.EnvoyFilter: "alpha-authn",
	} {
		obj := unstructured.Unstructured{}
		obj.SetGroupVersionKind(kind)
		obj.SetName(name)
		obj.SetAnnotations(map[string]string{"platform.opendatahub.io/instance.generation": "old"})
		g.Expect(gatewayAuthResourceCollectable(rr, obj)).To(BeFalse(), name)
	}
	for _, name := range []string{"beta-auth-creds", "kube-auth-proxy-beta", AuthClientID + "-beta", "beta-authn", "unrelated"} {
		obj := unstructured.Unstructured{}
		obj.SetName(name)
		obj.SetAnnotations(map[string]string{"platform.opendatahub.io/instance.generation": "old"})
		g.Expect(gatewayAuthResourceCollectable(rr, obj)).To(BeTrue(), name)
	}
	obj := unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk.KubernetesGateway)
	obj.SetName("alpha-auth-creds")
	obj.SetAnnotations(map[string]string{"platform.opendatahub.io/instance.generation": "old"})
	g.Expect(gatewayAuthResourceCollectable(rr, obj)).To(BeTrue(), "a removed Gateway must not be protected by a failed target's Secret name")
}

func TestPreparedAuthSecretSurvivesDeploymentUntilIngressRemoval(t *testing.T) {
	g := NewWithT(t)
	original := cluster.GetClusterInfo()
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
	t.Cleanup(func() { cluster.SetClusterInfo(original) })
	ingress := additionalIngress("alpha", "alpha.example.com", "alpha")
	config := newGatewayAuthTestConfig(ingress)
	credentials := map[string][]byte{
		EnvClientID: []byte(AuthClientID + "-alpha"), EnvClientSecret: []byte("client-secret"), EnvCookieSecret: []byte("cookie-secret"),
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "alpha-auth-creds", Namespace: GetGatewayNamespace()},
		Data:       credentials,
	}
	setGatewayConfigOwner(secret, config)
	cli := newGatewayTestClient(t, config, secret)
	rr := &odhtypes.ReconciliationRequest{
		Client: cli, Instance: config,
		Release: common.Release{Name: cluster.OpenDataHub},
		Controller: mocks.NewMockController(func(m *mocks.MockController) {
			m.On("Owns", mock.Anything).Return(true)
		}),
	}
	g.Expect(prepareAuthProxy(t.Context(), rr, config, additionalAuthProxyTarget(ingress, config),
		cluster.AuthModeIntegratedOAuth, kubeAuthProxyDeploymentOauthTemplate)).To(Succeed())
	g.Expect(deploy.NewAction(deploy.WithMode(deploy.ModePatch))(t.Context(), rr)).To(Succeed())
	g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(secret), secret)).To(Succeed())
	g.Expect(secret.Data).To(Equal(credentials), "deployment must preserve the persisted client and cookie credentials")
	g.Expect(secret.Annotations).To(HaveKeyWithValue("platform.opendatahub.io/instance.generation", "1"))
	g.Expect(secret.Annotations).To(HaveKeyWithValue("platform.opendatahub.io/instance.uid", "config"))
	g.Expect(secret.Annotations).To(HaveKeyWithValue("platform.opendatahub.io/type", string(cluster.OpenDataHub)))
	g.Expect(secret.Annotations).To(HaveKeyWithValue("platform.opendatahub.io/version", "0.0.0"))
	obj, err := resources.ToUnstructured(secret)
	g.Expect(err).NotTo(HaveOccurred())
	obj.SetGroupVersionKind(gvk.Secret)
	g.Expect(gatewayAuthResourceCollectable(rr, *obj)).To(BeFalse(), "active credentials must survive garbage collection")
	config.Spec.AdditionalIngresses = nil
	config.Generation++
	g.Expect(gatewayAuthResourceCollectable(rr, *obj)).To(BeTrue(), "removed ingress credentials must be collectable")
}

func TestForeignAuthenticationDependenciesStopReconciliation(t *testing.T) {
	original := cluster.GetClusterInfo()
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
	t.Cleanup(func() { cluster.SetClusterInfo(original) })
	for _, kind := range []schema.GroupVersionKind{gvk.EnvoyFilter, gvk.Service, gvk.Deployment, gvk.ServiceAccount, gvk.DestinationRule} {
		for _, missingCredentials := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/fallback=%t", kind.Kind, missingCredentials), func(t *testing.T) {
				g := NewWithT(t)
				ingress := additionalIngress("alpha", "alpha.example.com", "alpha")
				config := newGatewayAuthTestConfig(ingress)
				target := additionalAuthProxyTarget(ingress, config)
				foreign := &unstructured.Unstructured{}
				foreign.SetGroupVersionKind(kind)
				foreign.SetName(target.resourceNames()[kind])
				foreign.SetNamespace(GetGatewayNamespace())
				objects := []client.Object{foreign}
				if missingCredentials {
					objects = append(objects, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: target.SecretName, Namespace: GetGatewayNamespace()}})
				}
				rr := newGatewayAuthTestRequest(t, config, objects...)
				g.Expect(syncAdditionalIngressStatus(t.Context(), rr)).To(Succeed())
				err := prepareAuthProxies(t.Context(), rr, config, cluster.AuthModeIntegratedOAuth, kubeAuthProxyDeploymentOauthTemplate, []authProxyTarget{target})
				g.Expect(err).To(MatchError(ContainSubstring("not controlled by this GatewayConfig")))
				var requeue actionerrors.RequeueAfterError
				g.Expect(errors.As(err, &requeue)).To(BeFalse(), "a deferred retry would continue to bridge Route deployment")
			})
		}
	}
}

func TestAuthenticationResourcesAreIndependentPerGateway(t *testing.T) {
	for _, mode := range []string{"IntegratedOAuth", "OIDC"} {
		t.Run(mode, func(t *testing.T) {
			g := NewWithT(t)
			original := cluster.GetClusterInfo()
			cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
			t.Cleanup(func() { cluster.SetClusterInfo(original) })
			alpha := additionalIngress("alpha", "alpha.example.com", "alpha")
			beta := additionalIngress("beta", "beta.example.com", "beta")
			gamma := additionalIngress("gamma", "gamma.example.com", "gamma")
			two := int32(2)
			alpha.Auth.MaxReplicas = &two
			config := newGatewayAuthTestConfig(alpha, beta, gamma)
			objects := []client.Object{&configv1.Authentication{
				ObjectMeta: metav1.ObjectMeta{Name: cluster.ClusterAuthenticationObj},
				Spec:       configv1.AuthenticationSpec{Type: configv1.AuthenticationType(mode)},
			}}
			for i, name := range []string{"default", "alpha", "beta", "gamma"} {
				ref := corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: name + "-source"},
					Key:                  "secret",
				}
				objects = append(objects, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: ref.Name, Namespace: GetGatewayNamespace()},
					Data:       map[string][]byte{"secret": []byte(name + "-client")},
				})
				if mode != "OIDC" {
					continue
				}
				if i == 0 {
					config.Spec.OIDC = &serviceApi.OIDCConfig{
						IssuerURL: "https://issuer.example.com", ClientID: name, ClientSecretRef: ref,
					}
				} else {
					config.Spec.AdditionalIngresses[i-1].Auth.OIDC = &serviceApi.AdditionalIngressOIDCConfig{
						ClientID: name, ClientSecretRef: ref,
					}
				}
			}
			rr := newGatewayAuthTestRequest(t, config, objects...)
			cli := rr.Client
			g.Expect(syncAdditionalIngressStatus(t.Context(), rr)).To(Succeed())
			g.Expect(createKubeAuthProxyInfrastructure(t.Context(), rr)).To(Succeed())
			g.Expect(createEnvoyFilter(t.Context(), rr)).To(Succeed())
			g.Expect(createNetworkPolicy(t.Context(), rr)).To(Succeed())

			g.Expect(template.NewAction(template.WithDataFn(getTemplateData), template.WithNamespaceFn(func(context.Context, *odhtypes.ReconciliationRequest) (string, error) {
				return GetGatewayNamespace(), nil
			}))(t.Context(), rr)).To(Succeed())
			deployments := map[string]*appsv1.Deployment{}
			hpas := map[string]*autoscalingv2.HorizontalPodAutoscaler{}
			credentials := 0
			for _, obj := range rr.Resources {
				switch obj.GetKind() {
				case gvk.Deployment.Kind:
					dep := &appsv1.Deployment{}
					g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, dep)).To(Succeed())
					deployments[dep.Name] = dep
				case gvk.Secret.Kind:
					credentials++
				case gvk.HorizontalPodAutoscaler.Kind:
					hpa := &autoscalingv2.HorizontalPodAutoscaler{}
					g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, hpa)).To(Succeed())
					hpas[hpa.Name] = hpa
				case gvk.ClusterRoleBinding.Kind:
					binding := &rbacv1.ClusterRoleBinding{}
					g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, binding)).To(Succeed())
					g.Expect(binding.Subjects).To(HaveLen(1))
					g.Expect(binding.Name).To(Equal(binding.Subjects[0].Name + "-tokenreview"))
				case gvk.HTTPRoute.Kind:
					route := &gwapiv1.HTTPRoute{}
					g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, route)).To(Succeed())
					g.Expect(route.Spec.Hostnames).To(BeEmpty())
					g.Expect(route.Spec.ParentRefs).To(HaveLen(1))
					g.Expect(route.Spec.ParentRefs[0].SectionName).To(BeNil())
				case gvk.DestinationRule.Kind:
					host, _, err := unstructured.NestedString(obj.Object, "spec", "host")
					g.Expect(err).NotTo(HaveOccurred())
					if obj.GetName() == DestinationRuleName {
						g.Expect(host).To(Equal("*"))
					} else {
						name := strings.TrimSuffix(obj.GetName(), "-auth-destination")
						g.Expect(host).To(Equal("kube-auth-proxy-" + name + "." + GetGatewayNamespace() + ".svc.cluster.local"))
					}
				case gvk.EnvoyFilter.Kind:
					g.Expect(fmt.Sprint(obj.Object)).NotTo(ContainSubstring("failure_mode_allow"))
				}
			}
			g.Expect(deployments).To(HaveLen(4))
			g.Expect(credentials).To(Equal(4), "generated credentials need deployment metadata for garbage collection")
			g.Expect(hpas).To(HaveLen(4))
			g.Expect(deployments[KubeAuthProxyName].Spec.Template.Spec.Containers[0].Args).To(ContainElement("--cookie-domain=rh-ai.apps.example.com"))
			for _, name := range []string{"", "alpha", "beta", "gamma"} {
				target := defaultAuthProxyTarget(config.Spec.Domain, config)
				if name != "" {
					target = additionalAuthProxyTarget(additionalIngress(name, name+".example.com", name), config)
				}
				secret := &corev1.Secret{}
				g.Expect(cli.Get(t.Context(), types.NamespacedName{
					Name:      target.SecretName,
					Namespace: GetGatewayNamespace(),
				}, secret)).To(Succeed())
				g.Expect(deployments[target.ProxyName].Spec.Template.Annotations["opendatahub.io/secret-hash"]).To(Equal(calculateAuthConfigHash(secret)),
					"each proxy must roll out using its own freshly persisted credentials")
				if name == "" {
					g.Expect(rr.Extensions[target.extensionsKey()]).To(Equal(calculateAuthConfigHash(secret)),
						"additional proxy preparation must preserve the default proxy hash")
				}
			}
			for _, name := range []string{"alpha", "beta", "gamma"} {
				dep := deployments["kube-auth-proxy-"+name]
				g.Expect(dep).NotTo(BeNil())
				g.Expect(dep.Spec.Template.Spec.ServiceAccountName).To(Equal("kube-auth-proxy-" + name))
				args := dep.Spec.Template.Spec.Containers[0].Args
				g.Expect(args).To(ContainElement("--redirect-url=https://" + name + ".example.com/oauth2/callback"))
				g.Expect(args).To(ContainElement("--cookie-name=" + AuthProxyCookieName + "_" + name))
				g.Expect(args).To(ContainElement("--cookie-domain=" + name + ".example.com"))
				g.Expect(dep.Spec.Template.Spec.Containers[0].Env[0].ValueFrom.SecretKeyRef.Name).To(Equal(name + "-auth-creds"))
				g.Expect(hpas[dep.Name].Spec.MinReplicas).NotTo(BeNil())
				g.Expect(*hpas[dep.Name].Spec.MinReplicas).To(Equal(int32(2)))
				g.Expect(hpas[dep.Name].Spec.ScaleTargetRef.Name).To(Equal(dep.Name))
			}
			g.Expect(hpas["kube-auth-proxy-alpha"].Spec.MaxReplicas).To(Equal(int32(2)))
			g.Expect(hpas["kube-auth-proxy-beta"].Spec.MaxReplicas).To(Equal(int32(10)))
			g.Expect(hpas["kube-auth-proxy-gamma"].Spec.MaxReplicas).To(Equal(int32(10)))
		})
	}
}

func TestGatewayDeployRejectsBridgeRoutesWhenAuthenticationFilterFails(t *testing.T) {
	for _, failure := range []string{"credentials", "filter"} {
		t.Run(failure, func(t *testing.T) {
			g := NewWithT(t)
			config := newGatewayAuthTestConfig(
				additionalIngress("alpha", "alpha.example.com", "alpha"),
				additionalIngress("beta", "beta.example.com", "beta"),
			)
			var attempted []string
			cli, err := fakeclient.New(fakeclient.WithObjects(config), fakeclient.WithInterceptorFuncs(interceptor.Funcs{
				Apply: func(_ context.Context, _ client.WithWatch, obj runtime.ApplyConfiguration, _ ...client.ApplyOption) error {
					// Reject a target filter before any bridge Route can be applied.
					u, ok := obj.(client.Object)
					if !ok {
						return fmt.Errorf("unexpected apply configuration %T", obj)
					}
					attempted = append(attempted, u.GetObjectKind().GroupVersionKind().Kind+"/"+u.GetName())
					if u.GetName() == "alpha-authn" {
						return errors.New("admission rejected authentication filter")
					}
					return nil
				},
			}))
			g.Expect(err).NotTo(HaveOccurred())
			rr := &odhtypes.ReconciliationRequest{
				Client:     cli,
				Instance:   config,
				Conditions: conditionstest.NewManager(config, ReadyConditionType, serviceApi.AdditionalGatewaysReadyConditionType),
				Controller: mocks.NewMockController(func(m *mocks.MockController) { m.On("Owns", mock.Anything).Return(true) }),
			}
			g.Expect(syncAdditionalIngressStatus(t.Context(), rr)).To(Succeed())
			// Preparation failures still render the fallback filter for the standard deploy action.
			if failure == "credentials" {
				// Existing credentials owned outside GatewayConfig block preparation.
				g.Expect(cli.Create(t.Context(), &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "alpha-auth-creds",
						Namespace: GetGatewayNamespace(),
					},
				})).To(Succeed())
			}
			prepareErr := prepareAuthProxies(t.Context(), rr, config, cluster.AuthModeIntegratedOAuth,
				kubeAuthProxyDeploymentOauthTemplate, []authProxyTarget{
					additionalAuthProxyTarget(config.Spec.AdditionalIngresses[0], config),
					additionalAuthProxyTarget(config.Spec.AdditionalIngresses[1], config),
				})
			if failure == "credentials" {
				g.Expect(prepareErr).To(MatchError(ContainSubstring("requeue after")))
			} else {
				g.Expect(prepareErr).NotTo(HaveOccurred())
			}
			g.Expect(createAdditionalIngressRoutes(t.Context(), rr, config)).To(Succeed())
			for _, name := range []string{"alpha", "beta"} {
				gateway := managedGatewayForIngress(name)
				gateway.SetGroupVersionKind(gvk.KubernetesGateway)
				g.Expect(rr.AddResources(gateway)).To(Succeed())
			}
			// A previously reported readiness failure must not become a lifecycle gate.
			recordAuthFailure(config, "beta", additionalIngressReasonAuthenticationUnavailable, "Rollout pending")
			err = deploy.NewAction(deploy.WithCache())(t.Context(), rr)
			var requeue actionerrors.RequeueAfterError
			g.Expect(err).To(HaveOccurred())
			g.Expect(errors.As(err, &requeue)).To(BeFalse(), "deployment errors must be reported for reconciliation to retry")
			g.Expect(attempted).To(ContainElement(gvk.EnvoyFilter.Kind + "/alpha-authn"))
			g.Expect(attempted).NotTo(ContainElement(HavePrefix(gvk.Route.Kind+"/")), "bridge Routes must follow successful filter application")
		})
	}
}
