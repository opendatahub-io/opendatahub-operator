//go:build !integration

//nolint:testpackage
package gateway

import (
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"

	. "github.com/onsi/gomega"
)

func TestBuildAdditionalIngressRouteUsesHTTPSPort(t *testing.T) {
	g := NewWithT(t)
	ingress := additionalIngress("alpha", "alpha.apps.example.com", "alpha")
	route, err := buildAdditionalIngressRoute(ingress)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(route.Name).To(Equal(ingress.Name))
	g.Expect(route.Namespace).To(Equal(GetGatewayNamespace()))
	g.Expect(route.Labels).To(HaveKeyWithValue("example.com/ingress", "alpha"))
	g.Expect(route.Spec.Host).To(Equal(ingress.Hostname))
	g.Expect(route.Spec.To.Kind).To(Equal("Service"))
	g.Expect(route.Spec.To.Name).To(Equal(GetGatewayServiceFullName(ingress.Name)))
	g.Expect(route.Spec.Port.TargetPort).To(Equal(intstr.FromInt(StandardHTTPSPort)))
	g.Expect(route.Spec.TLS.Termination).To(Equal(routev1.TLSTerminationReencrypt))
	g.Expect(route.Spec.TLS.InsecureEdgeTerminationPolicy).To(Equal(routev1.InsecureEdgeTerminationPolicyRedirect))
	g.Expect(route.Annotations).To(HaveKeyWithValue(serviceCAAnnotation, "true"))
	g.Expect(route.Annotations).NotTo(HaveKey("haproxy.router.openshift.io/timeout"))
}

func TestCreateAdditionalIngressRoutesUsePerIngressGatewayServices(t *testing.T) {
	g := NewWithT(t)
	alpha := additionalIngress("alpha", "alpha.apps.example.com", "alpha")
	beta := additionalIngress("beta", "beta.apps.example.com", "beta")
	ingresses := serviceApi.AdditionalIngresses{alpha, beta}
	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName, UID: types.UID("config-uid"), Generation: 1},
		Spec:       serviceApi.GatewayConfigSpec{AdditionalIngresses: ingresses},
	}
	gatewayConfig.Status.AdditionalIngresses = buildAdditionalIngressStatuses(ingresses, nil, gatewayConfig.Generation)

	defaultNamespace := gwapiv1.Namespace(GetGatewayNamespace())
	defaultHTTPRoute := &gwapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "component-route", Namespace: GetGatewayNamespace()},
		Spec: gwapiv1.HTTPRouteSpec{CommonRouteSpec: gwapiv1.CommonRouteSpec{
			ParentRefs: []gwapiv1.ParentReference{{
				Name:      gwapiv1.ObjectName(GetDefaultGatewayName()),
				Namespace: &defaultNamespace,
			}},
		}},
	}

	cli := newGatewayTestClient(t, gatewayConfig, defaultHTTPRoute)
	rr := &odhtypes.ReconciliationRequest{Client: cli, Instance: gatewayConfig}

	g.Expect(createAdditionalIngressRoutes(t.Context(), rr, gatewayConfig)).To(Succeed())
	g.Expect(rr.Resources).To(HaveLen(2))

	routesByName := make(map[string]unstructured.Unstructured, len(rr.Resources))
	for _, resource := range rr.Resources {
		routesByName[resource.GetName()] = resource
	}
	for _, ingress := range []serviceApi.AdditionalIngress{alpha, beta} {
		serviceName, found, err := unstructured.NestedString(
			routesByName[ingress.Name].Object, "spec", "to", "name")
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(found).To(BeTrue())
		g.Expect(serviceName).To(Equal(GetGatewayServiceFullName(ingress.Name)))
	}
	observedHTTPRoute := &gwapiv1.HTTPRoute{}
	g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(defaultHTTPRoute), observedHTTPRoute)).To(Succeed())
	g.Expect(observedHTTPRoute.Spec.ParentRefs).To(HaveLen(1))
	g.Expect(observedHTTPRoute.Spec.ParentRefs[0].Name).To(Equal(gwapiv1.ObjectName(GetDefaultGatewayName())))
	g.Expect(observedHTTPRoute.Spec.ParentRefs[0].SectionName).To(BeNil())
}

func TestCreateAdditionalIngressRoutesDoesNotAdoptUnownedRoute(t *testing.T) {
	g := NewWithT(t)
	ingress := additionalIngress("alpha", "alpha.apps.example.com", "alpha")
	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName, UID: types.UID("config-uid"), Generation: 1},
		Spec:       serviceApi.GatewayConfigSpec{AdditionalIngresses: serviceApi.AdditionalIngresses{ingress}},
	}
	gatewayConfig.Status.AdditionalIngresses = buildAdditionalIngressStatuses(
		gatewayConfig.Spec.AdditionalIngresses, nil, gatewayConfig.Generation,
	)
	existing, err := buildAdditionalIngressRoute(ingress)
	g.Expect(err).NotTo(HaveOccurred())
	cli := newGatewayTestClient(t, gatewayConfig, existing)
	rr := &odhtypes.ReconciliationRequest{Client: cli, Instance: gatewayConfig}

	g.Expect(createAdditionalIngressRoutes(t.Context(), rr, gatewayConfig)).To(Succeed())
	g.Expect(rr.Resources).To(BeEmpty())
	g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(existing), &routev1.Route{})).To(Succeed())
	condition := conditions.FindStatusCondition(
		additionalIngressStatusByName(gatewayConfig, ingress.Name), serviceApi.AdditionalIngressRouteAdmittedConditionType,
	)
	g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(condition.Reason).To(Equal(additionalIngressReasonOwnershipConflict))
}

func TestIsGatewayCertificateOrReferencedSecret(t *testing.T) {
	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName},
		Spec: serviceApi.GatewayConfigSpec{
			Certificate: &infrav1.CertificateSpec{Type: infrav1.Provided, SecretName: "gateway-cert"},
			OIDC: &serviceApi.OIDCConfig{ClientSecretRef: corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "oidc-client"},
			}},
			ProviderCASecretName: "provider-ca",
		},
	}
	cli := newGatewayTestClient(t, gatewayConfig)
	for _, test := range []struct {
		name string
		want bool
	}{
		{name: "gateway-cert", want: true},
		{name: "oidc-client", want: true},
		{name: "provider-ca", want: true},
		{name: "unrelated"},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: test.name, Namespace: GetGatewayNamespace()}}
			g.Expect(isGatewayCertificateOrReferencedSecret(t.Context(), cli, secret, GetGatewayNamespace())).
				To(Equal(test.want))
		})
	}
}
