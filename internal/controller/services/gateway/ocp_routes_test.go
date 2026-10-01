//go:build !integration

//nolint:testpackage
package gateway

import (
	"context"
	stderrors "errors"
	"strings"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	metadatalabels "github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestBuildAdditionalIngressRoute(t *testing.T) {
	g := NewWithT(t)
	ingress := serviceApi.AdditionalIngress{
		Name:                  "alpha",
		Hostname:              "alpha.apps.example.com",
		ListenerPort:          9443,
		IngressControllerName: "alpha",
		RouteLabels:           map[string]string{"example.com/ingress": "alpha"},
	}
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "gateway-provider"}}

	route := mustBuildAdditionalIngressRoute(t, ingress, service)

	g.Expect(route.Name).To(Equal(GetAdditionalIngressRouteName(ingress.Name)))
	g.Expect(route.Namespace).To(Equal(GetGatewayNamespace()))
	g.Expect(route.Labels).To(HaveKeyWithValue("example.com/ingress", "alpha"))
	g.Expect(route.Labels).To(HaveKeyWithValue(metadatalabels.K8SCommon.PartOf, PartOfGatewayConfig))
	g.Expect(route.Labels).NotTo(HaveKey(metadatalabels.PlatformPartOf))
	g.Expect(route.Annotations).To(HaveKeyWithValue(serviceCAAnnotation, "true"))
	g.Expect(route.Spec.Host).To(Equal(ingress.Hostname))
	g.Expect(route.Spec.To.Kind).To(Equal("Service"))
	g.Expect(route.Spec.To.Name).To(Equal(service.Name))
	g.Expect(route.Spec.To.Weight).NotTo(BeNil())
	g.Expect(*route.Spec.To.Weight).To(Equal(int32(100)))
	g.Expect(route.Spec.Port).NotTo(BeNil())
	g.Expect(route.Spec.Port.TargetPort.IntVal).To(Equal(ingress.ListenerPort))
	g.Expect(route.Spec.TLS).NotTo(BeNil())
	g.Expect(route.Spec.TLS.Termination).To(Equal(routev1.TLSTerminationReencrypt))
	g.Expect(route.Spec.TLS.InsecureEdgeTerminationPolicy).To(Equal(routev1.InsecureEdgeTerminationPolicyRedirect))
}

func TestGetAdditionalIngressRouteNameIsStableAndBounded(t *testing.T) {
	g := NewWithT(t)
	longName := strings.Repeat("a", 63)
	otherLongName := strings.Repeat("b", 63)

	name := GetAdditionalIngressRouteName(longName)

	g.Expect(name).To(Equal(GetAdditionalIngressRouteName(longName)))
	g.Expect(name).To(HaveLen(63))
	g.Expect(name).To(MatchRegexp(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`))
	g.Expect(GetAdditionalIngressRouteName(otherLongName)).NotTo(Equal(name))
}

func TestIsGatewayCertificateOrReferencedSecret(t *testing.T) {
	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName},
		Spec: serviceApi.GatewayConfigSpec{
			Certificate: &infrav1.CertificateSpec{
				Type:       infrav1.Provided,
				SecretName: "gateway-cert",
			},
			OIDC: &serviceApi.OIDCConfig{
				ClientSecretRef: corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "oidc-client"},
				},
			},
			ProviderCASecretName: "provider-ca",
		},
	}
	cli := newGatewayTestClient(t, gatewayConfig)
	tests := []struct {
		name      string
		secret    *corev1.Secret
		wantMatch bool
	}{
		{
			name: "gateway certificate",
			secret: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Name: "gateway-cert", Namespace: GetGatewayNamespace(),
			}},
			wantMatch: true,
		},
		{
			name: "OIDC client secret",
			secret: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Name: "oidc-client", Namespace: GetGatewayNamespace(),
			}},
			wantMatch: true,
		},
		{
			name: "provider CA secret",
			secret: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Name: "provider-ca", Namespace: GetGatewayNamespace(),
			}},
			wantMatch: true,
		},
		{
			name: "unrelated secret",
			secret: &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
				Name: "unrelated", Namespace: GetGatewayNamespace(),
			}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			matched := isGatewayCertificateOrReferencedSecret(t.Context(), cli, test.secret, GetGatewayNamespace())
			g.Expect(matched).To(Equal(test.wantMatch))
		})
	}
}

func TestCreateAdditionalIngressRoutesSkipsUnavailableIngressController(t *testing.T) {
	g := NewWithT(t)
	alpha := additionalIngress("alpha", "alpha.other.example.com", 9443, "alpha")
	beta := additionalIngress("beta", "beta.apps.example.com", 9444, "missing")
	service := gatewayProviderService(9443, 9444)
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: GetGatewayNamespace()}}
	controller := alphaIngressController()
	ingresses := serviceApi.AdditionalIngresses{alpha, beta}
	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName, UID: types.UID("gateway-config-uid"), Generation: 1},
		Spec:       serviceApi.GatewayConfigSpec{AdditionalIngresses: ingresses},
	}
	gatewayConfig.Status.AdditionalIngresses = buildAdditionalIngressStatuses(ingresses, nil, gatewayConfig.Generation)

	cli := newGatewayTestClient(t, gatewayConfig, service, namespace, controller, managedGateway())
	rr := &odhtypes.ReconciliationRequest{Client: cli, Instance: gatewayConfig}

	g.Expect(createAdditionalIngressRoutes(t.Context(), rr, ingresses)).To(Succeed())

	g.Expect(rr.Resources).To(HaveLen(1))
	g.Expect(rr.Resources[0].GetName()).To(Equal(GetAdditionalIngressRouteName(alpha.Name)))
	g.Expect(additionalIngressRouteServices(rr)[alpha.Name]).To(Equal(service.Name))
	alphaStatus := additionalIngressStatusByName(gatewayConfig, alpha.Name)
	g.Expect(syncAdditionalIngressRouteReadiness(t.Context(), rr, gatewayConfig, alpha, service.Name, alphaStatus)).To(Succeed())
	alphaCondition := conditions.FindStatusCondition(alphaStatus, serviceApi.AdditionalIngressRouteAdmittedConditionType)
	g.Expect(alphaCondition.Status).To(Equal(metav1.ConditionUnknown))
	g.Expect(alphaCondition.Reason).To(Equal(serviceApi.AdditionalIngressReconciliationPendingReason))
	condition := conditions.FindStatusCondition(additionalIngressStatusByName(gatewayConfig, beta.Name),
		serviceApi.AdditionalIngressRouteAdmittedConditionType)
	g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(condition.Reason).To(Equal(additionalIngressReasonDependencyUnavailable))
	g.Expect(condition.Message).NotTo(BeEmpty())
}

func TestCreateAdditionalIngressRoutesPreservesAdmittedCondition(t *testing.T) {
	g := NewWithT(t)
	ingress := additionalIngress("alpha", "alpha.apps.example.com", 9443, "alpha")
	service := gatewayProviderService(9443)
	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName, UID: types.UID("gateway-config-uid"), Generation: 5},
		Spec:       serviceApi.GatewayConfigSpec{AdditionalIngresses: serviceApi.AdditionalIngresses{ingress}},
	}
	route := mustBuildAdditionalIngressRoute(t, ingress, service)
	setGatewayConfigOwner(route, gatewayConfig)
	route.Status.Ingress = []routev1.RouteIngress{{
		Host:       ingress.Hostname,
		RouterName: ingress.IngressControllerName,
		Conditions: []routev1.RouteIngressCondition{{Type: routev1.RouteAdmitted, Status: corev1.ConditionTrue}},
	}}
	admitted := additionalIngressRouteCondition(route, ingress, gatewayConfig.Generation)
	admitted.LastTransitionTime = metav1.NewTime(time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC))
	gatewayConfig.Status.AdditionalIngresses = []serviceApi.AdditionalIngressStatus{{
		Name: ingress.Name, Hostname: ingress.Hostname, Conditions: []common.Condition{admitted},
	}}
	status := additionalIngressStatusByName(gatewayConfig, ingress.Name)

	cli := newGatewayTestClient(t, gatewayConfig, service,
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: GetGatewayNamespace()}},
		alphaIngressController(), route, managedGateway())
	rr := &odhtypes.ReconciliationRequest{Client: cli, Instance: gatewayConfig}

	g.Expect(createAdditionalIngressRoutes(t.Context(), rr, gatewayConfig.Spec.AdditionalIngresses)).To(Succeed())
	g.Expect(conditions.FindStatusCondition(status, serviceApi.AdditionalIngressRouteAdmittedConditionType)).To(Equal(&admitted))
	g.Expect(syncAdditionalIngressRouteReadiness(t.Context(), rr, gatewayConfig, ingress,
		additionalIngressRouteServices(rr)[ingress.Name], status)).To(Succeed())
	g.Expect(conditions.FindStatusCondition(status, serviceApi.AdditionalIngressRouteAdmittedConditionType)).To(Equal(&admitted))
}

func TestFindGatewayServiceAcceptsNonControllerOwnerReference(t *testing.T) {
	for _, test := range []struct {
		name       string
		apiVersion string
	}{
		{name: "v1", apiVersion: gwapiv1.GroupVersion.String()},
		{name: "v1beta1", apiVersion: "gateway.networking.k8s.io/v1beta1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			service := gatewayProviderService(9443)
			service.OwnerReferences[0].APIVersion = test.apiVersion
			service.OwnerReferences[0].Controller = nil
			service.OwnerReferences[0].BlockOwnerDeletion = nil
			cli := newGatewayTestClient(t, service, managedGateway())

			foundService, serviceUnavailableMessage, err := findGatewayService(t.Context(), cli)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(serviceUnavailableMessage).To(BeEmpty())
			g.Expect(foundService.Name).To(Equal(service.Name))
		})
	}
}

func TestFindGatewayServiceRejectsDifferentGatewayOwner(t *testing.T) {
	g := NewWithT(t)
	service := gatewayProviderService(9443)
	service.OwnerReferences[0].UID = types.UID("another-gateway")
	cli := newGatewayTestClient(t, service, managedGateway())

	_, serviceUnavailableMessage, err := findGatewayService(t.Context(), cli)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(serviceUnavailableMessage).To(ContainSubstring("not owned"))
}

func TestFindGatewayServiceRejectsDifferentGatewayAPIGroup(t *testing.T) {
	g := NewWithT(t)
	service := gatewayProviderService(9443)
	service.OwnerReferences[0].APIVersion = "other.gateway.networking.k8s.io/v1"
	cli := newGatewayTestClient(t, service, managedGateway())

	_, serviceUnavailableMessage, err := findGatewayService(t.Context(), cli)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(serviceUnavailableMessage).To(ContainSubstring("not owned"))
}

func TestCreateAdditionalIngressRoutesReportsUnavailableService(t *testing.T) {
	for _, test := range []struct {
		name     string
		services []client.Object
		message  string
	}{
		{name: "missing", message: "No shared Gateway Service"},
		{name: "ambiguous", services: []client.Object{
			gatewayProviderService(9443),
			func() *corev1.Service {
				service := gatewayProviderService(9443)
				service.Name = "gateway-provider-replacement"
				return service
			}(),
		}, message: "Multiple shared Gateway Services (2)"},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			ingress := additionalIngress("alpha", "alpha.apps.example.com", 9443, "alpha")
			gatewayConfig := &serviceApi.GatewayConfig{
				ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName, Generation: 1},
				Spec:       serviceApi.GatewayConfigSpec{AdditionalIngresses: serviceApi.AdditionalIngresses{ingress}},
			}
			gatewayConfig.Status.AdditionalIngresses = buildAdditionalIngressStatuses(
				gatewayConfig.Spec.AdditionalIngresses, nil, gatewayConfig.Generation)
			objects := append([]client.Object{managedGateway()}, test.services...)
			cli := newGatewayTestClient(t, objects...)
			rr := &odhtypes.ReconciliationRequest{Client: cli, Instance: gatewayConfig}

			g.Expect(createAdditionalIngressRoutes(t.Context(), rr, gatewayConfig.Spec.AdditionalIngresses)).To(Succeed())
			g.Expect(rr.Resources).To(BeEmpty())
			condition := conditions.FindStatusCondition(additionalIngressStatusByName(gatewayConfig, ingress.Name),
				serviceApi.AdditionalIngressRouteAdmittedConditionType)
			g.Expect(condition.Status).To(Equal(metav1.ConditionUnknown))
			g.Expect(condition.Message).To(ContainSubstring(test.message))
		})
	}
}

func TestCreateAdditionalIngressRoutesIsolatesMissingPort(t *testing.T) {
	g := NewWithT(t)
	alpha := additionalIngress("alpha", "alpha.apps.example.com", 9443, "alpha")
	beta := additionalIngress("beta", "beta.apps.example.com", 9444, "beta")
	ingresses := serviceApi.AdditionalIngresses{alpha, beta}
	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName, Generation: 1},
		Spec:       serviceApi.GatewayConfigSpec{AdditionalIngresses: ingresses},
	}
	gatewayConfig.Status.AdditionalIngresses = buildAdditionalIngressStatuses(ingresses, nil, gatewayConfig.Generation)
	betaController := alphaIngressController()
	betaController.Name = "beta"
	cli := newGatewayTestClient(t, gatewayConfig, gatewayProviderService(9443), managedGateway(),
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: GetGatewayNamespace()}},
		alphaIngressController(), betaController)
	rr := &odhtypes.ReconciliationRequest{Client: cli, Instance: gatewayConfig}

	g.Expect(createAdditionalIngressRoutes(t.Context(), rr, ingresses)).To(Succeed())
	g.Expect(rr.Resources).To(HaveLen(1))
	g.Expect(rr.Resources[0].GetName()).To(Equal(GetAdditionalIngressRouteName(alpha.Name)))
	condition := conditions.FindStatusCondition(additionalIngressStatusByName(gatewayConfig, beta.Name),
		serviceApi.AdditionalIngressRouteAdmittedConditionType)
	g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(condition.Message).To(ContainSubstring("does not expose listener port 9444"))
}

func TestCreateAdditionalIngressRoutesDoesNotPreflightIngressSelectors(t *testing.T) {
	for _, siblingName := range []string{"default", "sibling"} {
		t.Run(siblingName, func(t *testing.T) {
			g := NewWithT(t)
			ingress := additionalIngress("alpha", "alpha.other.example.com", 9443, "alpha")
			service := gatewayProviderService(9443)
			namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: GetGatewayNamespace()}}
			target := alphaIngressController()
			target.Spec.RouteSelector.MatchLabels["example.com/ingress"] = "other"
			sibling := &operatorv1.IngressController{
				ObjectMeta: metav1.ObjectMeta{Name: siblingName, Namespace: cluster.IngressControllerName.Namespace},
				Spec: operatorv1.IngressControllerSpec{
					Domain: "alpha.apps.example.com",
					RouteSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
						"example.com/ingress": "alpha",
					}},
				},
			}

			cli := newGatewayTestClient(t, service, namespace, target, sibling, managedGateway())
			rr := &odhtypes.ReconciliationRequest{Client: cli}

			g.Expect(createAdditionalIngressRoutes(t.Context(), rr, serviceApi.AdditionalIngresses{ingress})).To(Succeed())
			g.Expect(rr.Resources).To(HaveLen(1))
		})
	}
}

func TestCreateAdditionalIngressRoutesKeepsNonTargetAdmission(t *testing.T) {
	g := NewWithT(t)
	ingress := additionalIngress("alpha", "alpha.apps.example.com", 9443, "alpha")
	service := gatewayProviderService(9443)
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: GetGatewayNamespace()}}
	target := alphaIngressController()
	route := mustBuildAdditionalIngressRoute(t, ingress, service)
	gatewayConfig := &serviceApi.GatewayConfig{ObjectMeta: metav1.ObjectMeta{
		Name: serviceApi.GatewayConfigName, UID: types.UID("gateway-config-uid"),
	}}
	setGatewayConfigOwner(route, gatewayConfig)
	route.Status.Ingress = []routev1.RouteIngress{{
		Host:       ingress.Hostname,
		RouterName: "sibling",
		Conditions: []routev1.RouteIngressCondition{{
			Type:   routev1.RouteAdmitted,
			Status: corev1.ConditionTrue,
		}},
	}}

	cli := newGatewayTestClient(t, service, namespace, target, route, gatewayConfig, managedGateway())
	rr := &odhtypes.ReconciliationRequest{Client: cli, Instance: gatewayConfig}

	g.Expect(createAdditionalIngressRoutes(t.Context(), rr, serviceApi.AdditionalIngresses{ingress})).To(Succeed())

	g.Expect(rr.Resources).To(HaveLen(1))
}

func TestCreateAdditionalIngressRoutesRetriesRouteOwnershipReadFailure(t *testing.T) {
	g := NewWithT(t)
	ingress := additionalIngress("alpha", "alpha.apps.example.com", 9443, "alpha")
	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName, Generation: 5},
		Spec:       serviceApi.GatewayConfigSpec{AdditionalIngresses: serviceApi.AdditionalIngresses{ingress}},
	}
	gatewayConfig.Status.AdditionalIngresses = buildAdditionalIngressStatuses(
		gatewayConfig.Spec.AdditionalIngresses, nil, gatewayConfig.Generation)
	cli, err := fakeclient.New(
		fakeclient.WithObjects(gatewayConfig, gatewayProviderService(9443),
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: GetGatewayNamespace()}},
			alphaIngressController(), managedGateway()),
		fakeclient.WithInterceptorFuncs(interceptor.Funcs{
			Get: func(ctx context.Context, cli client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*routev1.Route); ok {
					return stderrors.New("temporary Route read failure")
				}
				return cli.Get(ctx, key, obj, opts...)
			},
		}),
	)
	g.Expect(err).NotTo(HaveOccurred())
	rr := &odhtypes.ReconciliationRequest{Client: cli, Instance: gatewayConfig}

	err = createAdditionalIngressRoutes(t.Context(), rr, gatewayConfig.Spec.AdditionalIngresses)
	var requeue odherrors.RequeueAfterError
	g.Expect(stderrors.As(err, &requeue)).To(BeTrue())
	g.Expect(rr.Resources).To(BeEmpty())
	condition := conditions.FindStatusCondition(additionalIngressStatusByName(gatewayConfig, ingress.Name),
		serviceApi.AdditionalIngressRouteAdmittedConditionType)
	g.Expect(condition.Status).To(Equal(metav1.ConditionUnknown))
	g.Expect(condition.Reason).To(Equal(additionalIngressReasonStatusReadFailed))
}

func TestCleanupAdditionalIngressRoutesDeletesStaleOwnedRoute(t *testing.T) {
	g := NewWithT(t)
	setGatewayClusterType(t, cluster.ClusterTypeOpenShift)

	gatewayConfig := &serviceApi.GatewayConfig{ObjectMeta: metav1.ObjectMeta{
		Name: serviceApi.GatewayConfigName,
		UID:  types.UID("gateway-config-uid"),
	}}
	staleRoute := mustBuildAdditionalIngressRoute(t,
		additionalIngress("alpha", "alpha.apps.example.com", 9443, "missing"),
		gatewayProviderService(9443),
	)
	setGatewayConfigOwner(staleRoute, gatewayConfig)
	defaultRoute := &routev1.Route{ObjectMeta: metav1.ObjectMeta{
		Name: GetDefaultGatewayName(), Namespace: GetGatewayNamespace(),
	}}
	setGatewayConfigOwner(defaultRoute, gatewayConfig)

	cli := newGatewayTestClient(t, gatewayConfig, staleRoute, defaultRoute)
	rr := &odhtypes.ReconciliationRequest{Client: cli, Instance: gatewayConfig}

	g.Expect(cleanupAdditionalIngressRoutes(t.Context(), rr)).To(Succeed())

	err := cli.Get(t.Context(), client.ObjectKeyFromObject(staleRoute), &routev1.Route{})
	g.Expect(k8serr.IsNotFound(err)).To(BeTrue())
	g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(defaultRoute), &routev1.Route{})).To(Succeed())
}

func TestCleanupAdditionalIngressRoutesRetriesInterruptedDelete(t *testing.T) {
	g := NewWithT(t)
	setGatewayClusterType(t, cluster.ClusterTypeOpenShift)
	gatewayConfig := &serviceApi.GatewayConfig{ObjectMeta: metav1.ObjectMeta{
		Name: serviceApi.GatewayConfigName, UID: types.UID("gateway-config-uid"),
	}}
	staleRoute := mustBuildAdditionalIngressRoute(t,
		additionalIngress("alpha", "alpha.apps.example.com", 9443, "alpha"), gatewayProviderService(9443))
	setGatewayConfigOwner(staleRoute, gatewayConfig)
	failed := false
	cli, err := fakeclient.New(
		fakeclient.WithObjects(gatewayConfig, staleRoute),
		fakeclient.WithInterceptorFuncs(interceptor.Funcs{
			Delete: func(ctx context.Context, cli client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				if !failed {
					failed = true
					return stderrors.New("temporary delete failure")
				}
				return cli.Delete(ctx, obj, opts...)
			},
		}),
	)
	g.Expect(err).NotTo(HaveOccurred())
	rr := &odhtypes.ReconciliationRequest{Client: cli, Instance: gatewayConfig}

	g.Expect(cleanupAdditionalIngressRoutes(t.Context(), rr)).To(MatchError(ContainSubstring("temporary delete failure")))
	g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(staleRoute), &routev1.Route{})).To(Succeed())
	g.Expect(cleanupAdditionalIngressRoutes(t.Context(), rr)).To(Succeed())
	g.Expect(k8serr.IsNotFound(cli.Get(t.Context(), client.ObjectKeyFromObject(staleRoute), &routev1.Route{}))).To(BeTrue())
}

func TestCleanupAdditionalIngressRoutesKeepsDesiredOwnedRoute(t *testing.T) {
	g := NewWithT(t)
	setGatewayClusterType(t, cluster.ClusterTypeOpenShift)

	service := gatewayProviderService(9443)
	ingress := additionalIngress("alpha", "alpha.apps.example.com", 9443, "alpha")
	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.GatewayConfigName,
			UID:  types.UID("gateway-config-uid"),
		},
		Spec: serviceApi.GatewayConfigSpec{
			IngressMode:         serviceApi.IngressModeOcpRoute,
			AdditionalIngresses: serviceApi.AdditionalIngresses{ingress},
		},
	}
	gatewayConfig.Status.AdditionalIngresses = buildAdditionalIngressStatuses(
		gatewayConfig.Spec.AdditionalIngresses, nil, gatewayConfig.Generation,
	)
	setAdditionalIngressCondition(additionalIngressStatusByName(gatewayConfig, ingress.Name), gatewayConfig.Generation,
		serviceApi.AdditionalIngressRouteAdmittedConditionType, metav1.ConditionUnknown,
		additionalIngressReasonDependencyUnavailable, "Cluster ingress domain is not available yet")
	desiredRoute := mustBuildAdditionalIngressRoute(t, additionalIngress("alpha", "alpha.apps.example.com", 9443, "alpha"), service)
	staleRoute := mustBuildAdditionalIngressRoute(t, additionalIngress("beta", "beta.apps.example.com", 9444, "beta"), service)
	setGatewayConfigOwner(desiredRoute, gatewayConfig)
	setGatewayConfigOwner(staleRoute, gatewayConfig)

	cli := newGatewayTestClient(t, gatewayConfig, desiredRoute, staleRoute)
	rr := &odhtypes.ReconciliationRequest{Client: cli, Instance: gatewayConfig}
	additionalIngressRouteServices(rr)[ingress.Name] = service.Name
	g.Expect(rr.AddResources(desiredRoute)).To(Succeed())

	g.Expect(cleanupAdditionalIngressRoutes(t.Context(), rr)).To(Succeed())
	g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(desiredRoute), &routev1.Route{})).To(Succeed())
	g.Expect(k8serr.IsNotFound(cli.Get(t.Context(), client.ObjectKeyFromObject(staleRoute), &routev1.Route{}))).To(BeTrue())
	setAdditionalIngressCondition(additionalIngressStatusByName(gatewayConfig, ingress.Name), gatewayConfig.Generation,
		serviceApi.AdditionalIngressRouteAdmittedConditionType, metav1.ConditionFalse,
		"RouteNotAdmitted", "Router has not admitted the Route")
	g.Expect(cleanupAdditionalIngressRoutes(t.Context(), rr)).To(Succeed())
	g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(desiredRoute), &routev1.Route{})).To(Succeed())
	gatewayConfig.Spec.IngressMode = serviceApi.IngressModeLoadBalancer
	g.Expect(cleanupAdditionalIngressRoutes(t.Context(), rr)).To(Succeed())
	g.Expect(k8serr.IsNotFound(cli.Get(t.Context(), client.ObjectKeyFromObject(desiredRoute), &routev1.Route{}))).To(BeTrue())
}

func TestCleanupAdditionalIngressRoutesDeletesRouteForConfirmedIngressFailure(t *testing.T) {
	g := NewWithT(t)
	setGatewayClusterType(t, cluster.ClusterTypeOpenShift)
	ingress := additionalIngress("alpha", "alpha.apps.example.com", 9443, "missing")
	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.GatewayConfigName, UID: types.UID("gateway-config-uid"), Generation: 1,
		},
		Spec: serviceApi.GatewayConfigSpec{
			IngressMode:         serviceApi.IngressModeOcpRoute,
			AdditionalIngresses: serviceApi.AdditionalIngresses{ingress},
		},
	}
	gatewayConfig.Status.AdditionalIngresses = buildAdditionalIngressStatuses(
		gatewayConfig.Spec.AdditionalIngresses, nil, gatewayConfig.Generation,
	)
	route := mustBuildAdditionalIngressRoute(t, ingress, gatewayProviderService(9443))
	setGatewayConfigOwner(route, gatewayConfig)
	cli := newGatewayTestClient(t, gatewayConfig, route)
	rr := &odhtypes.ReconciliationRequest{Client: cli, Instance: gatewayConfig}
	rejectAdditionalIngressRoute(rr, ingress.Name,
		additionalIngressReasonDependencyUnavailable, "Target IngressController was not found")

	g.Expect(cleanupAdditionalIngressRoutes(t.Context(), rr)).To(Succeed())
	g.Expect(k8serr.IsNotFound(cli.Get(t.Context(), client.ObjectKeyFromObject(route), &routev1.Route{}))).To(BeTrue())
}

func TestCleanupAdditionalIngressRoutesSkipsKubernetes(t *testing.T) {
	g := NewWithT(t)
	setGatewayClusterType(t, cluster.ClusterTypeKubernetes)

	gatewayConfig := &serviceApi.GatewayConfig{ObjectMeta: metav1.ObjectMeta{
		Name: serviceApi.GatewayConfigName,
		UID:  types.UID("gateway-config-uid"),
	}}
	staleRoute := mustBuildAdditionalIngressRoute(t,
		additionalIngress("alpha", "alpha.apps.example.com", 9443, "alpha"),
		gatewayProviderService(9443),
	)
	setGatewayConfigOwner(staleRoute, gatewayConfig)

	cli := newGatewayTestClient(t, gatewayConfig, staleRoute)
	rr := &odhtypes.ReconciliationRequest{Client: cli, Instance: gatewayConfig}

	g.Expect(cleanupAdditionalIngressRoutes(t.Context(), rr)).To(Succeed())
	g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(staleRoute), &routev1.Route{})).To(Succeed())
}

func TestCreateAdditionalIngressRoutesDoesNotAdoptUnownedRoute(t *testing.T) {
	g := NewWithT(t)
	ingress := additionalIngress("alpha", "alpha.apps.example.com", 9443, "alpha")
	service := gatewayProviderService(9443)
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: GetGatewayNamespace()}}
	target := alphaIngressController()
	existing := mustBuildAdditionalIngressRoute(t, ingress, service)
	gatewayConfig := &serviceApi.GatewayConfig{ObjectMeta: metav1.ObjectMeta{
		Name: serviceApi.GatewayConfigName,
		UID:  types.UID("gateway-config-uid"),
	}}

	cli := newGatewayTestClient(t, service, namespace, target, existing, gatewayConfig, managedGateway())
	rr := &odhtypes.ReconciliationRequest{Client: cli, Instance: gatewayConfig}

	g.Expect(createAdditionalIngressRoutes(t.Context(), rr, serviceApi.AdditionalIngresses{ingress})).To(Succeed())
	g.Expect(rr.Resources).To(BeEmpty())
	g.Expect(cli.Get(t.Context(), client.ObjectKeyFromObject(existing), &routev1.Route{})).To(Succeed())
}

func TestIsOwnedByGatewayConfig(t *testing.T) {
	gatewayConfig := &serviceApi.GatewayConfig{ObjectMeta: metav1.ObjectMeta{
		Name: serviceApi.GatewayConfigName,
		UID:  types.UID("gateway-config-uid"),
	}}
	tests := []struct {
		name      string
		mutate    func(*routev1.Route)
		wantOwned bool
	}{
		{name: "matches controller owner", wantOwned: true},
		{
			name: "rejects different owner name",
			mutate: func(route *routev1.Route) {
				route.OwnerReferences[0].Name = "another-config"
			},
		},
		{
			name: "rejects different owner kind",
			mutate: func(route *routev1.Route) {
				route.OwnerReferences[0].Kind = "Other"
			},
		},
		{
			name: "rejects different owner UID",
			mutate: func(route *routev1.Route) {
				route.OwnerReferences[0].UID = types.UID("another-uid")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			route := &routev1.Route{}
			setGatewayConfigOwner(route, gatewayConfig)
			if test.mutate != nil {
				test.mutate(route)
			}
			g.Expect(isOwnedByGatewayConfig(route, gatewayConfig)).To(Equal(test.wantOwned))
		})
	}
}

func mustBuildAdditionalIngressRoute(t *testing.T, ingress serviceApi.AdditionalIngress, service *corev1.Service) *routev1.Route {
	t.Helper()
	route, err := buildAdditionalIngressRoute(ingress, service)
	if err != nil {
		t.Fatalf("failed to build additional ingress Route: %v", err)
	}
	return route
}

func setGatewayClusterType(t *testing.T, clusterType string) {
	t.Helper()
	cluster.SetClusterInfo(cluster.ClusterInfo{Type: clusterType})
	t.Cleanup(func() { cluster.SetClusterInfo(cluster.ClusterInfo{}) })
}

func gatewayProviderService(ports ...int32) *corev1.Service {
	servicePorts := make([]corev1.ServicePort, 0, len(ports))
	for _, port := range ports {
		servicePorts = append(servicePorts, corev1.ServicePort{Port: port})
	}
	controller := true
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "gateway-provider",
			Namespace: GetGatewayNamespace(),
			Labels: map[string]string{
				metadatalabels.GatewayAPI.GatewayName: GetDefaultGatewayName(),
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         gwapiv1.GroupVersion.String(),
				Kind:               "Gateway",
				Name:               GetDefaultGatewayName(),
				UID:                types.UID("gateway-uid"),
				Controller:         &controller,
				BlockOwnerDeletion: &controller,
			}},
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: "10.0.0.10",
			Ports:     servicePorts,
		},
	}
}

func managedGateway() *gwapiv1.Gateway {
	return &gwapiv1.Gateway{ObjectMeta: metav1.ObjectMeta{
		Name:      GetDefaultGatewayName(),
		Namespace: GetGatewayNamespace(),
		UID:       types.UID("gateway-uid"),
	}}
}

func alphaIngressController() *operatorv1.IngressController {
	return &operatorv1.IngressController{
		ObjectMeta: metav1.ObjectMeta{Name: "alpha", Namespace: cluster.IngressControllerName.Namespace},
		Spec: operatorv1.IngressControllerSpec{
			Domain: "alpha.apps.example.com",
			RouteSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
				"example.com/ingress": "alpha",
			}},
		},
	}
}
