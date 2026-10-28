//go:build !integration

//nolint:testpackage
package gateway

import (
	"context"
	"encoding/json"
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
	k8serr "k8s.io/apimachinery/pkg/api/errors"
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

func TestAdditionalIngressResourcesUseStandardDeployment(t *testing.T) {
	g := NewWithT(t)
	original := cluster.GetClusterInfo()
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
	t.Cleanup(func() { cluster.SetClusterInfo(original) })
	ingress := additionalIngress("alpha", "alpha.example.com", "alpha")
	config := newGatewayAuthTestConfig(ingress)
	gateway := managedGatewayForIngress("alpha")
	setGatewayConfigOwner(gateway, config)
	filter := resources.GvkToUnstructured(gvk.EnvoyFilter)
	filter.SetName("alpha-authn")
	filter.SetNamespace(GetGatewayNamespace())
	filter.SetOwnerReferences(gateway.OwnerReferences)
	route, err := buildAdditionalIngressRoute(ingress)
	g.Expect(err).NotTo(HaveOccurred())
	setGatewayConfigOwner(route, config)
	rr := newGatewayAuthTestRequest(t, config, gateway, filter, route,
		func() *corev1.ConfigMap {
			obj := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "alpha-config", Namespace: GetGatewayNamespace()}}
			setGatewayConfigOwner(obj, config)
			return obj
		}(),
		func() *corev1.Secret {
			obj := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "alpha-auth-creds", Namespace: GetGatewayNamespace()},
				Data: map[string][]byte{
					EnvClientSecret: []byte("existing-client-secret"), EnvCookieSecret: []byte("existing-cookie-secret"),
				}}
			setGatewayConfigOwner(obj, config)
			return obj
		}(),
		&configv1.Authentication{
			ObjectMeta: metav1.ObjectMeta{Name: cluster.ClusterAuthenticationObj},
			Spec:       configv1.AuthenticationSpec{Type: configv1.AuthenticationTypeIntegratedOAuth},
		})
	g.Expect(syncAdditionalIngressStatus(t.Context(), rr)).To(Succeed())
	setAdditionalIngressCondition(additionalIngressStatusByName(config, "alpha"), config.Generation,
		serviceApi.AdditionalIngressGatewayReadyConditionType, metav1.ConditionFalse,
		additionalIngressReasonOwnershipConflict, "Conflict reported by an earlier operator version")
	g.Expect(createGatewayInfrastructure(t.Context(), rr)).To(Succeed())
	g.Expect(createKubeAuthProxyInfrastructure(t.Context(), rr)).To(Succeed())
	g.Expect(createAdditionalIngressRoutes(t.Context(), rr, config)).To(Succeed())
	rendered := make([]string, 0, len(rr.Resources))
	for _, obj := range rr.Resources {
		rendered = append(rendered, obj.GetKind()+"/"+obj.GetName())
	}
	g.Expect(rendered).To(ContainElements("Gateway/alpha", "ConfigMap/alpha-config", "Route/alpha",
		"EnvoyFilter/alpha-authn", "Deployment/kube-auth-proxy-alpha", "Secret/alpha-auth-creds", "OAuthClient/"+AuthClientID+"-alpha"))
	secret := &corev1.Secret{}
	g.Expect(rr.Client.Get(t.Context(), client.ObjectKey{Name: "alpha-auth-creds", Namespace: GetGatewayNamespace()}, secret)).To(Succeed())
	g.Expect(string(secret.Data[EnvClientSecret])).To(Equal("existing-client-secret"))
	g.Expect(string(secret.Data[EnvCookieSecret])).To(Equal("existing-cookie-secret"))
}

func TestProviderCARotationChangesAuthProxyDeploymentHashes(t *testing.T) {
	g := NewWithT(t)
	original := cluster.GetClusterInfo()
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
	t.Cleanup(func() { cluster.SetClusterInfo(original) })
	config := newGatewayAuthTestConfig(additionalIngress("alpha", "alpha.example.com", "alpha"))
	config.Spec.ProviderCASecretName = "provider-ca"
	caSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "provider-ca", Namespace: GetGatewayNamespace()},
		Data:       map[string][]byte{"ca.crt": []byte("ca-v1")},
	}
	rr := newGatewayAuthTestRequest(t, config, caSecret, &configv1.Authentication{
		ObjectMeta: metav1.ObjectMeta{Name: cluster.ClusterAuthenticationObj},
		Spec:       configv1.AuthenticationSpec{Type: configv1.AuthenticationTypeIntegratedOAuth},
	})
	renderHashes := func() map[string]string {
		rr.Resources, rr.Templates, rr.Extensions = nil, nil, nil
		g.Expect(createKubeAuthProxyInfrastructure(t.Context(), rr)).To(Succeed())
		hashes := map[string]string{}
		for _, obj := range rr.Resources {
			if obj.GetKind() != gvk.Deployment.Kind {
				continue
			}
			deployment := &appsv1.Deployment{}
			g.Expect(runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, deployment)).To(Succeed())
			hashes[deployment.Name] = deployment.Spec.Template.Annotations["opendatahub.io/secret-hash"]
		}
		g.Expect(hashes).To(HaveLen(2))
		return hashes
	}
	before := renderHashes()
	g.Expect(before[KubeAuthProxyName]).NotTo(Equal(before[KubeAuthProxyName+"-alpha"]), "each proxy must retain its own credential hash")
	g.Expect(renderHashes()).To(Equal(before), "unchanged CA and credentials must keep the rollout hashes stable")
	g.Expect(rr.Client.Get(t.Context(), client.ObjectKeyFromObject(caSecret), caSecret)).To(Succeed())
	caSecret.Data["ca.crt"] = []byte("ca-v2")
	g.Expect(rr.Client.Update(t.Context(), caSecret)).To(Succeed())
	after := renderHashes()
	for _, name := range []string{KubeAuthProxyName, KubeAuthProxyName + "-alpha"} {
		g.Expect(before[name]).NotTo(BeEmpty(), name)
		g.Expect(after[name]).NotTo(BeEmpty(), name)
		g.Expect(after[name]).NotTo(Equal(before[name]), "CA rotation must roll out %s", name)
	}
	g.Expect(rr.Client.Get(t.Context(), client.ObjectKeyFromObject(caSecret), caSecret)).To(Succeed())
	g.Expect(caSecret.Data["ca.crt"]).To(Equal([]byte("ca-v2")), "the supplied CA must not be reverted")
	caSecret.Data["ca.crt"] = []byte("ca-v1")
	g.Expect(rr.Client.Update(t.Context(), caSecret)).To(Succeed())
	g.Expect(renderHashes()).To(Equal(before), "restoring the original CA must restore both rollout hashes")
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
	g.Expect(rr.Resources).To(HaveLen(2), "None preserves a filter for each Gateway without deploying a proxy")
	for i, name := range []string{AuthnFilterName, "alpha-authn"} {
		g.Expect(rr.Resources[i].GroupVersionKind()).To(Equal(gvk.EnvoyFilter))
		g.Expect(rr.Resources[i].GetName()).To(Equal(name))
	}
	g.Expect(rr.Templates).To(BeEmpty())
	g.Expect(rr.Client.Get(t.Context(), types.NamespacedName{
		Name:      "alpha-auth-creds",
		Namespace: GetGatewayNamespace(),
	}, &corev1.Secret{})).To(MatchError(ContainSubstring("not found")))
	status := additionalIngressStatusByName(config, ingress.Name)
	g.Expect(syncAdditionalIngressAuthReadiness(t.Context(), rr, config, ingress)).To(BeFalse())
	updateAdditionalGatewaysReadyCondition(rr, config)
	condition := conditions.FindStatusCondition(status, serviceApi.AdditionalIngressAuthenticationReadyConditionType)
	g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(condition.Reason).To(Equal(additionalIngressReasonAuthenticationBlocked))
	g.Expect(createAdditionalIngressRoutes(t.Context(), rr, config)).To(Succeed())
	g.Expect(rr.Resources).To(HaveLen(3))
	g.Expect(rr.Resources[2].GetKind()).To(Equal(gvk.Route.Kind))
}

func TestAdditionalAuthReconciliationPreservesTransitionTime(t *testing.T) {
	g := NewWithT(t)
	original := cluster.GetClusterInfo()
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
	t.Cleanup(func() { cluster.SetClusterInfo(original) })
	ingress := additionalIngress("alpha", "alpha.example.com", "alpha")
	config := newGatewayAuthTestConfig(ingress)
	proxy := readyAuthProxyForIngress("alpha")
	setGatewayConfigOwner(proxy, config)
	rr := newGatewayAuthTestRequest(t, config, proxy, &configv1.Authentication{
		ObjectMeta: metav1.ObjectMeta{Name: cluster.ClusterAuthenticationObj},
		Spec:       configv1.AuthenticationSpec{Type: configv1.AuthenticationTypeIntegratedOAuth},
	})
	g.Expect(syncAdditionalIngressStatus(t.Context(), rr)).To(Succeed())
	status := additionalIngressStatusByName(config, "alpha")
	for _, kind := range []string{serviceApi.AdditionalIngressGatewayReadyConditionType, serviceApi.AdditionalIngressRouteAdmittedConditionType} {
		setAdditionalIngressCondition(status, config.Generation, kind, metav1.ConditionTrue, "Ready", "Ready")
	}
	g.Expect(syncAdditionalIngressAuthReadiness(t.Context(), rr, config, ingress)).To(BeFalse())
	updateAdditionalGatewaysReadyCondition(rr, config)
	previous := metav1.Unix(100, 0)
	for i := range status.Conditions {
		status.Conditions[i].LastTransitionTime = previous
	}
	// A new pass must not publish a temporary Pending condition before observing
	// that the existing proxy is still available.
	rr.Extensions = nil
	g.Expect(createKubeAuthProxyInfrastructure(t.Context(), rr)).To(Succeed())
	g.Expect(syncAdditionalIngressAuthReadiness(t.Context(), rr, config, ingress)).To(BeFalse())
	updateAdditionalGatewaysReadyCondition(rr, config)
	for _, kind := range []string{serviceApi.AdditionalIngressAuthenticationReadyConditionType, serviceApi.AdditionalIngressReadyConditionType} {
		condition := conditions.FindStatusCondition(status, kind)
		g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		g.Expect(condition.LastTransitionTime).To(Equal(previous), kind)
	}
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
	g.Expect(createKubeAuthProxyInfrastructure(t.Context(), rr)).To(Succeed())
	g.Expect(rr.Resources).To(HaveLen(2), "default and additional fail-closed filters remain")
	g.Expect(rr.Resources[1].GetKind()).To(Equal(gvk.EnvoyFilter.Kind))
	g.Expect(rr.Templates).To(BeEmpty())
	g.Expect(createAdditionalIngressRoutes(t.Context(), rr, config)).To(Succeed())
	g.Expect(rr.Resources).To(HaveLen(3))
	g.Expect(syncAdditionalIngressReadyStatuses(t.Context(), rr, false)).To(Succeed())
	g.Expect(conditions.FindStatusCondition(additionalIngressStatusByName(config, "alpha"),
		serviceApi.AdditionalIngressAuthenticationReadyConditionType).Reason).To(Equal(additionalIngressReasonInvalidAuthenticationConfig))
}

func TestAuthenticationCredentialReadErrorRetriesBeforeRendering(t *testing.T) {
	original := cluster.GetClusterInfo()
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
	t.Cleanup(func() { cluster.SetClusterInfo(original) })
	for name, readErr := range map[string]error{
		"API":       k8serr.NewServiceUnavailable("temporary API outage"),
		"transport": fmt.Errorf("read secret: %w", context.DeadlineExceeded),
	} {
		t.Run(name, func(t *testing.T) {
			g := NewWithT(t)
			ingress := additionalIngress("alpha", "alpha.example.com", "alpha")
			config := newGatewayAuthTestConfig(ingress)
			fail := true
			authentication := &configv1.Authentication{
				ObjectMeta: metav1.ObjectMeta{Name: cluster.ClusterAuthenticationObj},
				Spec:       configv1.AuthenticationSpec{Type: configv1.AuthenticationTypeIntegratedOAuth},
			}
			cli, err := fakeclient.New(fakeclient.WithObjects(config, authentication), fakeclient.WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, c client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if fail && key.Name == "alpha-auth-creds" {
						return readErr
					}
					return c.Get(ctx, key, obj, opts...)
				},
			}))
			g.Expect(err).NotTo(HaveOccurred())
			rr := newGatewayAuthTestRequest(t, config)
			rr.Client = cli
			g.Expect(syncAdditionalIngressStatus(t.Context(), rr)).To(Succeed())
			targets := []authProxyTarget{additionalAuthProxyTarget(ingress, config)}
			err = prepareAuthProxies(t.Context(), rr, config, cluster.AuthModeIntegratedOAuth, kubeAuthProxyDeploymentOauthTemplate, targets)
			g.Expect(errors.Is(err, readErr)).To(BeTrue(), "client errors must reach the reconciler's normal retry path: %v", err)
			g.Expect(rr.Resources).To(BeEmpty(), "a read error must not select a fallback authentication policy")
			g.Expect(syncAdditionalIngressReadyStatuses(t.Context(), rr, false)).To(Succeed())
			condition := conditions.FindStatusCondition(additionalIngressStatusByName(config, "alpha"), serviceApi.AdditionalIngressAuthenticationReadyConditionType)
			g.Expect(condition.Status).To(Equal(metav1.ConditionUnknown))
			g.Expect(condition.Reason).To(Equal(additionalIngressReasonStatusReadFailed))

			fail = false
			rr.Resources, rr.Templates, rr.Extensions = nil, nil, nil
			g.Expect(prepareAuthProxies(t.Context(), rr, config, cluster.AuthModeIntegratedOAuth, kubeAuthProxyDeploymentOauthTemplate, targets)).To(Succeed())
			g.Expect(rr.Resources).To(ContainElement(WithTransform(func(obj unstructured.Unstructured) string {
				return obj.GetKind() + "/" + obj.GetName()
			}, Equal("Deployment/kube-auth-proxy-alpha"))))
			g.Expect(syncAdditionalIngressReadyStatuses(t.Context(), rr, false)).To(Succeed())
			condition = conditions.FindStatusCondition(additionalIngressStatusByName(config, "alpha"), serviceApi.AdditionalIngressAuthenticationReadyConditionType)
			g.Expect(condition.Reason).To(Equal(serviceApi.AdditionalIngressReconciliationPendingReason))
		})
	}
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
			if failure == "alpha-config" {
				g.Expect(createKubeAuthProxyInfrastructure(t.Context(), rr)).To(Succeed())
			} else {
				g.Expect(createKubeAuthProxyInfrastructure(t.Context(), rr)).To(MatchError(ContainSubstring("requeue after")))
			}
			g.Expect(rr.Resources).To(ContainElement(WithTransform(func(obj unstructured.Unstructured) string {
				return obj.GetName()
			}, Equal("kube-auth-proxy-beta"))))
			g.Expect(syncAdditionalIngressReadyStatuses(t.Context(), rr, false)).To(Succeed())
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
	recordAuthResult(rr, additionalAuthProxyTarget(config.Spec.AdditionalIngresses[0], config), metav1.ConditionFalse,
		additionalIngressReasonAuthenticationUnavailable, "Credential preparation failed")
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
	obj.SetGroupVersionKind(gvk.Secret)
	obj.SetName("alpha-auth-creds")
	rr.Extensions = nil
	g.Expect(gatewayAuthResourceCollectable(rr, obj)).To(BeTrue(), "a removed target must return to normal garbage collection")
	for _, test := range []struct {
		status metav1.ConditionStatus
		reason string
	}{
		{metav1.ConditionUnknown, serviceApi.AdditionalIngressReconciliationPendingReason},
		{metav1.ConditionTrue, additionalIngressReasonAuthenticationDisabled},
		{metav1.ConditionFalse, additionalIngressReasonAuthenticationBlocked},
		{metav1.ConditionTrue, "Ready"},
		{metav1.ConditionFalse, "NotReady"},
	} {
		recordAuthResult(rr, additionalAuthProxyTarget(config.Spec.AdditionalIngresses[0], config), test.status, test.reason, "Readiness result")
		g.Expect(gatewayAuthResourceCollectable(rr, obj)).To(BeTrue(), "only preparation failures should prevent collection: %s", test.reason)
	}
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
	_, err := prepareAuthProxy(t.Context(), rr, config, additionalAuthProxyTarget(ingress, config),
		cluster.AuthModeIntegratedOAuth, kubeAuthProxyDeploymentOauthTemplate)
	g.Expect(err).NotTo(HaveOccurred())
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
			g.Expect(rr.Templates).To(BeEmpty())

			g.Expect(template.NewAction(template.WithDataFn(getTemplateData), template.WithNamespaceFn(func(context.Context, *odhtypes.ReconciliationRequest) (string, error) {
				return GetGatewayNamespace(), nil
			}))(t.Context(), rr)).To(Succeed())
			deployments := map[string]*appsv1.Deployment{}
			hpas := map[string]*autoscalingv2.HorizontalPodAutoscaler{}
			credentials := 0
			seen := map[string]bool{}
			for _, obj := range rr.Resources {
				key := obj.GroupVersionKind().String() + "/" + obj.GetNamespace() + "/" + obj.GetName()
				g.Expect(seen[key]).To(BeFalse(), "duplicate resource %s", key)
				seen[key] = true
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
					gatewayName, _, err := unstructured.NestedString(obj.Object, "spec", "workloadSelector", "labels", "gateway.networking.k8s.io/gateway-name")
					g.Expect(err).NotTo(HaveOccurred())
					proxyName := KubeAuthProxyName
					if gatewayName != GetDefaultGatewayName() {
						proxyName += "-" + gatewayName
					}
					g.Expect(fmt.Sprint(obj.Object)).To(ContainSubstring("https://" + proxyName + "." + GetGatewayNamespace() + ".svc.cluster.local:"))
				case gvk.Service.Kind:
					selector, _, err := unstructured.NestedString(obj.Object, "spec", "selector", "app")
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(selector).To(Equal(obj.GetName()))
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
	original := cluster.GetClusterInfo()
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
	t.Cleanup(func() { cluster.SetClusterInfo(original) })

	for _, failure := range []string{"credentials", "filter"} {
		t.Run(failure, func(t *testing.T) {
			g := NewWithT(t)
			config := newGatewayAuthTestConfig(
				additionalIngress("alpha", "alpha.example.com", "alpha"),
				additionalIngress("beta", "beta.example.com", "beta"),
			)
			var attempted []string
			authentication := &configv1.Authentication{
				ObjectMeta: metav1.ObjectMeta{Name: cluster.ClusterAuthenticationObj},
				Spec:       configv1.AuthenticationSpec{Type: configv1.AuthenticationTypeIntegratedOAuth},
			}
			cli, err := fakeclient.New(fakeclient.WithObjects(config, authentication), fakeclient.WithInterceptorFuncs(interceptor.Funcs{
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
				secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "alpha-auth-creds", Namespace: GetGatewayNamespace()}}
				setGatewayConfigOwner(secret, config)
				secret.OwnerReferences[0].Name = "other-gateway-config"
				secret.OwnerReferences[0].UID = "other-owner"
				g.Expect(cli.Create(t.Context(), secret)).To(Succeed())
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
			setAdditionalIngressCondition(additionalIngressStatusByName(config, "beta"), config.Generation,
				serviceApi.AdditionalIngressAuthenticationReadyConditionType, metav1.ConditionFalse,
				additionalIngressReasonAuthenticationUnavailable, "Rollout pending")
			err = deploy.NewAction(deploy.WithCache())(t.Context(), rr)
			var requeue actionerrors.RequeueAfterError
			g.Expect(err).To(HaveOccurred())
			g.Expect(errors.As(err, &requeue)).To(BeFalse(), "deployment errors must be reported for reconciliation to retry")
			g.Expect(attempted).To(ContainElement(gvk.EnvoyFilter.Kind + "/alpha-authn"))
			g.Expect(attempted).NotTo(ContainElement(HavePrefix(gvk.Route.Kind+"/")), "bridge Routes must follow successful filter application")
			g.Expect(syncAdditionalIngressReadyStatuses(t.Context(), rr, false)).To(Succeed())
			condition := conditions.FindStatusCondition(additionalIngressStatusByName(config, "alpha"), serviceApi.AdditionalIngressAuthenticationReadyConditionType)
			if failure == "credentials" {
				g.Expect(condition.Reason).To(Equal(additionalIngressReasonAuthenticationUnavailable))
			} else {
				g.Expect(condition.Status).To(Equal(metav1.ConditionUnknown))
				g.Expect(condition.Reason).To(Equal(serviceApi.AdditionalIngressReconciliationPendingReason))
			}
		})
	}
}

func TestDefaultAuthenticationRendersDuringPreparation(t *testing.T) {
	g := NewWithT(t)
	original := cluster.GetClusterInfo()
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
	t.Cleanup(func() { cluster.SetClusterInfo(original) })
	config := newGatewayAuthTestConfig()
	rr := newGatewayAuthTestRequest(t, config, &configv1.Authentication{
		ObjectMeta: metav1.ObjectMeta{Name: cluster.ClusterAuthenticationObj},
		Spec:       configv1.AuthenticationSpec{Type: configv1.AuthenticationTypeIntegratedOAuth},
	})
	g.Expect(createKubeAuthProxyInfrastructure(t.Context(), rr)).To(Succeed())
	g.Expect(rr.Templates).To(BeEmpty(), "all auth targets must be rendered by the same preparation action")
	counts := map[schema.GroupVersionKind]int{}
	for _, resource := range rr.Resources {
		counts[resource.GroupVersionKind()]++
	}
	for _, kind := range []schema.GroupVersionKind{gvk.Deployment, gvk.Service, gvk.Secret, gvk.OAuthClient, gvk.HTTPRoute, gvk.EnvoyFilter, gvk.NetworkPolicy} {
		g.Expect(counts[kind]).To(Equal(1), "expected one %s", kind.Kind)
	}
}

func TestDefaultAuthenticationPoliciesWithoutProxy(t *testing.T) {
	original := cluster.GetClusterInfo()
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: cluster.ClusterTypeOpenShift})
	t.Cleanup(func() { cluster.SetClusterInfo(original) })
	for _, mode := range []configv1.AuthenticationType{configv1.AuthenticationTypeNone, "OIDC"} {
		for _, enabled := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/network-policy=%t", mode, enabled), func(t *testing.T) {
				g := NewWithT(t)
				config := newGatewayAuthTestConfig()
				g.Expect(json.Unmarshal(fmt.Appendf(nil, `{"networkPolicy":{"ingress":{"enabled":%t}}}`, enabled), &config.Spec)).To(Succeed())
				rr := newGatewayAuthTestRequest(t, config, &configv1.Authentication{
					ObjectMeta: metav1.ObjectMeta{Name: cluster.ClusterAuthenticationObj},
					Spec:       configv1.AuthenticationSpec{Type: mode},
				})
				g.Expect(createKubeAuthProxyInfrastructure(t.Context(), rr)).To(Succeed())
				g.Expect(rr.Templates).To(BeEmpty())
				g.Expect(rr.Resources[0].GroupVersionKind()).To(Equal(gvk.EnvoyFilter))
				g.Expect(rr.Resources[0].GetName()).To(Equal(AuthnFilterName))
				g.Expect(rr.Resources).To(HaveLen(1), "the deprecated setting does not create a policy without a proxy")
				ready := rr.Conditions.GetCondition(ReadyConditionType)
				if mode == configv1.AuthenticationTypeNone {
					g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
				} else {
					g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
					g.Expect(ready.Message).To(Equal("Cluster is in OIDC mode but GatewayConfig has no OIDC configuration"))
				}
			})
		}
	}
}
