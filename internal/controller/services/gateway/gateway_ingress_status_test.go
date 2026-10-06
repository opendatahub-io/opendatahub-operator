//go:build !integration

//nolint:testpackage
package gateway

import (
	"context"
	stderrors "errors"
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestBuildAdditionalIngressStatuses(t *testing.T) {
	g := NewWithT(t)
	ready := common.Condition{
		Type:               serviceApi.AdditionalIngressGatewayReadyConditionType,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: 4,
		Reason:             additionalIngressReasonReady,
	}
	statuses := buildAdditionalIngressStatuses(
		[]serviceApi.AdditionalIngress{
			{Name: "alpha", Hostname: "alpha.example.com"},
			{Name: "beta", Hostname: "beta.example.com"},
		},
		[]serviceApi.AdditionalIngressStatus{{
			Name: "alpha", Conditions: []common.Condition{ready},
			GatewayRef: serviceApi.GatewayReference{Name: "alpha", Namespace: GetGatewayNamespace()},
		}, {
			Name: "beta", Conditions: []common.Condition{ready},
			GatewayRef: serviceApi.GatewayReference{Name: "odh-gw-beta-old", Namespace: GetGatewayNamespace()},
		}},
		4,
	)

	g.Expect(statuses).To(HaveLen(2))
	g.Expect(statuses[0].Name).To(Equal("alpha"))
	g.Expect(statuses[0].GatewayRef).To(Equal(serviceApi.GatewayReference{
		Name: "alpha", Namespace: GetGatewayNamespace(),
	}))
	g.Expect(statuses[0].Conditions[0]).To(Equal(ready))
	g.Expect(statuses[1].Name).To(Equal("beta"))
	g.Expect(statuses[1].GatewayRef).To(Equal(serviceApi.GatewayReference{
		Name: "beta", Namespace: GetGatewayNamespace(),
	}))
	g.Expect(statuses[1].Conditions).To(HaveLen(4))
	for _, condition := range statuses[1].Conditions {
		g.Expect(condition.Status).To(Equal(metav1.ConditionUnknown))
		g.Expect(condition.ObservedGeneration).To(Equal(int64(4)))
	}

	updated := buildAdditionalIngressStatuses([]serviceApi.AdditionalIngress{
		{Name: "beta", Hostname: "beta.example.com"},
		{Name: "alpha", Hostname: "new-alpha.example.com"},
	}, statuses, 5)
	g.Expect(updated[0].GatewayRef).To(Equal(statuses[1].GatewayRef))
	g.Expect(updated[1].Hostname).To(Equal("new-alpha.example.com"))
	g.Expect(updated[1].GatewayRef).To(Equal(statuses[0].GatewayRef))

	remaining := buildAdditionalIngressStatuses([]serviceApi.AdditionalIngress{
		{Name: "beta", Hostname: "beta.example.com"},
	}, updated, 6)
	g.Expect(remaining).To(HaveLen(1))
	g.Expect(remaining[0].GatewayRef).To(Equal(statuses[1].GatewayRef))
}

func TestAdditionalIngressGatewayReferenceSurvivesReadinessChanges(t *testing.T) {
	g := NewWithT(t)
	ingress := additionalIngress("alpha", "alpha.example.com", "alpha")
	config := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName, Generation: 5},
		Spec:       serviceApi.GatewayConfigSpec{AdditionalIngresses: serviceApi.AdditionalIngresses{ingress}},
	}
	cli := setupTestClient().WithStatusSubresource(&gwapiv1.Gateway{}).WithObjects(config).Build()
	rr := &odhtypes.ReconciliationRequest{Client: cli, Instance: config,
		Conditions: conditions.NewManager(config, status.ConditionTypeReady, ReadyConditionType, serviceApi.AdditionalGatewaysReadyConditionType)}
	rr.Conditions.MarkTrue(ReadyConditionType)
	g.Expect(syncAdditionalIngressStatus(t.Context(), rr)).To(Succeed())
	reference := serviceApi.GatewayReference{Name: ingress.Name, Namespace: GetGatewayNamespace()}
	check := func(expected metav1.ConditionStatus) {
		t.Helper()
		g.Expect(syncAdditionalIngressReadiness(t.Context(), rr)).To(Succeed())
		status := additionalIngressStatusByName(config, ingress.Name)
		g.Expect(status.GatewayRef).To(Equal(reference))
		g.Expect(conditions.FindStatusCondition(status, serviceApi.AdditionalIngressGatewayReadyConditionType).Status).
			To(Equal(expected))
		g.Expect(rr.Conditions.GetCondition(serviceApi.AdditionalGatewaysReadyConditionType).Status).To(Equal(expected))
		g.Expect(rr.Conditions.GetTopLevelCondition().Status).To(Equal(expected))
	}
	check(metav1.ConditionUnknown)
	gateway := managedGatewayForIngress(ingress.Name)
	g.Expect(cli.Create(t.Context(), gateway)).To(Succeed())
	check(metav1.ConditionTrue)
	gateway.Status.Conditions[0].Status = metav1.ConditionFalse
	g.Expect(cli.Status().Update(t.Context(), gateway)).To(Succeed())
	check(metav1.ConditionFalse)
	g.Expect(cli.Delete(t.Context(), gateway)).To(Succeed())
	check(metav1.ConditionUnknown)
	config.Spec.AdditionalIngresses = nil
	g.Expect(syncAdditionalIngressStatus(t.Context(), rr)).To(Succeed())
	g.Expect(rr.Conditions.GetCondition(serviceApi.AdditionalGatewaysReadyConditionType).Reason).To(Equal(additionalIngressReasonNoAdditionalGateways))
	g.Expect(rr.Conditions.IsHappy()).To(BeTrue())
}

func TestSyncAdditionalIngressReadinessUsesEachGateway(t *testing.T) {
	g := NewWithT(t)
	alpha := additionalIngress("alpha", "alpha.apps.example.com", "alpha")
	beta := additionalIngress("beta", "beta.apps.example.com", "beta")
	ingresses := serviceApi.AdditionalIngresses{alpha, beta}
	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.GatewayConfigName, UID: types.UID("gateway-config-uid"), Generation: 5,
		},
		Spec: serviceApi.GatewayConfigSpec{AdditionalIngresses: ingresses},
	}
	gatewayConfig.Status.AdditionalIngresses = buildAdditionalIngressStatuses(ingresses, nil, gatewayConfig.Generation)

	alphaRoute, err := buildAdditionalIngressRoute(alpha)
	g.Expect(err).NotTo(HaveOccurred())
	setGatewayConfigOwner(alphaRoute, gatewayConfig)
	alphaRoute.Status.Ingress = []routev1.RouteIngress{{
		Host: alpha.Hostname, RouterName: alpha.IngressControllerName,
		Conditions: []routev1.RouteIngressCondition{{Type: routev1.RouteAdmitted, Status: corev1.ConditionTrue}},
	}}

	cli := newGatewayTestClient(t, gatewayConfig,
		managedGatewayForIngress(alpha.Name), managedGatewayForIngress(beta.Name),
		alphaRoute)
	rr := &odhtypes.ReconciliationRequest{Client: cli, Instance: gatewayConfig,
		Conditions: conditions.NewManager(gatewayConfig, status.ConditionTypeReady, serviceApi.AdditionalGatewaysReadyConditionType)}

	g.Expect(syncAdditionalIngressReadiness(t.Context(), rr)).To(Succeed())
	g.Expect(syncAdditionalIngressReadyStatuses(t.Context(), rr, false)).To(Succeed())

	alphaStatus := additionalIngressStatusByName(gatewayConfig, alpha.Name)
	g.Expect(conditions.FindStatusCondition(alphaStatus, serviceApi.AdditionalIngressGatewayReadyConditionType).Status).
		To(Equal(metav1.ConditionTrue))
	g.Expect(conditions.FindStatusCondition(alphaStatus, serviceApi.AdditionalIngressRouteAdmittedConditionType).Status).
		To(Equal(metav1.ConditionTrue))
	g.Expect(conditions.FindStatusCondition(alphaStatus, serviceApi.AdditionalIngressAuthenticationReadyConditionType).Status).
		To(Equal(metav1.ConditionUnknown))
	g.Expect(conditions.FindStatusCondition(alphaStatus, serviceApi.AdditionalIngressReadyConditionType).Status).
		To(Equal(metav1.ConditionTrue))

	betaStatus := additionalIngressStatusByName(gatewayConfig, beta.Name)
	g.Expect(conditions.FindStatusCondition(betaStatus, serviceApi.AdditionalIngressGatewayReadyConditionType).Status).
		To(Equal(metav1.ConditionTrue))
	g.Expect(conditions.FindStatusCondition(betaStatus, serviceApi.AdditionalIngressRouteAdmittedConditionType).Status).
		To(Equal(metav1.ConditionUnknown))
	g.Expect(rr.Conditions.IsHappy()).To(BeTrue())
}

func TestSyncAdditionalIngressRouteReadinessReportsMissingAdmission(t *testing.T) {
	g := NewWithT(t)
	ingress := additionalIngress("alpha", "alpha.apps.example.com", "alpha")
	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName, UID: types.UID("gateway-config-uid"), Generation: 5},
	}
	route, err := buildAdditionalIngressRoute(ingress)
	g.Expect(err).NotTo(HaveOccurred())
	setGatewayConfigOwner(route, gatewayConfig)
	status := &serviceApi.AdditionalIngressStatus{}
	rr := &odhtypes.ReconciliationRequest{
		Client: newGatewayTestClient(t, route), Instance: gatewayConfig,
	}

	g.Expect(syncAdditionalIngressRouteReadiness(t.Context(), rr, gatewayConfig, ingress, status)).To(Succeed())
	condition := conditions.FindStatusCondition(status, serviceApi.AdditionalIngressRouteAdmittedConditionType)
	g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(condition.Reason).To(Equal(additionalIngressReasonNotReady))
}

func TestSyncAdditionalIngressRouteReadinessChecksHTTPSPort(t *testing.T) {
	ingress := additionalIngress("alpha", "alpha.apps.example.com", "alpha")
	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.GatewayConfigName, UID: types.UID("gateway-config-uid"), Generation: 5,
		},
	}
	route, err := buildAdditionalIngressRoute(ingress)
	if err != nil {
		t.Fatal(err)
	}
	setGatewayConfigOwner(route, gatewayConfig)
	route.Status.Ingress = []routev1.RouteIngress{{
		Host: ingress.Hostname, RouterName: ingress.IngressControllerName,
		Conditions: []routev1.RouteIngressCondition{{Type: routev1.RouteAdmitted, Status: corev1.ConditionTrue}},
	}}

	for _, test := range []struct {
		name       string
		mutate     func(*routev1.Route)
		wantStatus metav1.ConditionStatus
	}{
		{name: "matches HTTPS port", wantStatus: metav1.ConditionTrue},
		{name: "wrong HTTPS port", mutate: func(route *routev1.Route) {
			route.Spec.Port.TargetPort = intstr.FromInt(9443)
		}, wantStatus: metav1.ConditionFalse},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			currentRoute := route.DeepCopy()
			if test.mutate != nil {
				test.mutate(currentRoute)
			}
			rr := &odhtypes.ReconciliationRequest{
				Client: newGatewayTestClient(t, currentRoute), Instance: gatewayConfig,
			}
			status := &serviceApi.AdditionalIngressStatus{}

			g.Expect(syncAdditionalIngressRouteReadiness(t.Context(), rr, gatewayConfig, ingress, status)).To(Succeed())
			condition := conditions.FindStatusCondition(status, serviceApi.AdditionalIngressRouteAdmittedConditionType)
			g.Expect(condition.Status).To(Equal(test.wantStatus))
		})
	}
}

func TestAdditionalIngressRouteConditionReportsMultipleRouters(t *testing.T) {
	g := NewWithT(t)
	ingress := additionalIngress("alpha", "alpha.apps.example.com", "alpha")
	route := &routev1.Route{Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{
		{Host: ingress.Hostname, RouterName: "alpha", Conditions: []routev1.RouteIngressCondition{{
			Type: routev1.RouteAdmitted, Status: corev1.ConditionTrue,
		}}},
		{Host: ingress.Hostname, RouterName: "sibling", Conditions: []routev1.RouteIngressCondition{{
			Type: routev1.RouteAdmitted, Status: corev1.ConditionTrue,
		}}},
	}}}

	condition := additionalIngressRouteCondition(route, ingress, 4)
	g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
	g.Expect(condition.Reason).To(Equal(additionalIngressReasonMultipleControllers))
	g.Expect(condition.Severity).To(Equal(common.ConditionSeverityInfo))
}

func TestSyncAdditionalIngressReadinessRetriesReadFailures(t *testing.T) {
	g := NewWithT(t)
	ingress := additionalIngress("alpha", "alpha.apps.example.com", "alpha")
	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName, Generation: 5},
		Spec:       serviceApi.GatewayConfigSpec{AdditionalIngresses: serviceApi.AdditionalIngresses{ingress}},
	}
	gatewayConfig.Status.AdditionalIngresses = buildAdditionalIngressStatuses(
		gatewayConfig.Spec.AdditionalIngresses, nil, gatewayConfig.Generation)
	cli, err := fakeclient.New(fakeclient.WithInterceptorFuncs(interceptor.Funcs{
		Get: func(_ context.Context, _ client.WithWatch, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
			return stderrors.New("temporary API error")
		},
	}))
	g.Expect(err).NotTo(HaveOccurred())
	rr := &odhtypes.ReconciliationRequest{Client: cli, Instance: gatewayConfig,
		Conditions: conditions.NewManager(gatewayConfig, status.ConditionTypeReady, serviceApi.AdditionalGatewaysReadyConditionType)}

	err = syncAdditionalIngressReadiness(t.Context(), rr)
	var requeue odherrors.RequeueAfterError
	g.Expect(stderrors.As(err, &requeue)).To(BeTrue())
	status := additionalIngressStatusByName(gatewayConfig, ingress.Name)
	g.Expect(conditions.FindStatusCondition(status, serviceApi.AdditionalIngressGatewayReadyConditionType).Reason).
		To(Equal(additionalIngressReasonStatusReadFailed))
	g.Expect(conditions.FindStatusCondition(status, serviceApi.AdditionalIngressRouteAdmittedConditionType).Reason).
		To(Equal(additionalIngressReasonStatusReadFailed))
	g.Expect(rr.Conditions.GetCondition(serviceApi.AdditionalGatewaysReadyConditionType).Status).To(Equal(metav1.ConditionUnknown))
	g.Expect(rr.Conditions.IsHappy()).To(BeFalse())
}

func TestAdditionalGatewaysReadyAggregation(t *testing.T) {
	gatewayStatus := func(name string, value metav1.ConditionStatus, generation int64) serviceApi.AdditionalIngressStatus {
		return serviceApi.AdditionalIngressStatus{Name: name, Conditions: []common.Condition{{
			Type: serviceApi.AdditionalIngressGatewayReadyConditionType, Status: value, ObservedGeneration: generation,
		}}}
	}
	for _, test := range []struct {
		name     string
		statuses []serviceApi.AdditionalIngressStatus
		want     metav1.ConditionStatus
		message  string
	}{
		{name: "all ready", statuses: []serviceApi.AdditionalIngressStatus{
			gatewayStatus("alpha", metav1.ConditionTrue, 5), gatewayStatus("beta", metav1.ConditionTrue, 5),
		}, want: metav1.ConditionTrue, message: "All additional Gateways are ready"},
		{name: "failed takes precedence over pending", statuses: []serviceApi.AdditionalIngressStatus{
			gatewayStatus("alpha", metav1.ConditionUnknown, 5), gatewayStatus("beta", metav1.ConditionFalse, 5),
		}, want: metav1.ConditionFalse, message: "Additional Gateways are not ready: beta"},
		{name: "failure names follow configuration order", statuses: []serviceApi.AdditionalIngressStatus{
			gatewayStatus("beta", metav1.ConditionFalse, 5), gatewayStatus("alpha", metav1.ConditionFalse, 5),
		}, want: metav1.ConditionFalse, message: "Additional Gateways are not ready: alpha, beta"},
		{name: "missing status", want: metav1.ConditionUnknown, message: "Additional Gateway readiness is pending: alpha, beta"},
		{name: "missing condition", statuses: []serviceApi.AdditionalIngressStatus{
			{Name: "alpha"}, gatewayStatus("beta", metav1.ConditionTrue, 5),
		}, want: metav1.ConditionUnknown, message: "Additional Gateway readiness is pending: alpha"},
		{name: "stale conditions", statuses: []serviceApi.AdditionalIngressStatus{
			gatewayStatus("alpha", metav1.ConditionTrue, 4), gatewayStatus("beta", metav1.ConditionFalse, 4),
		}, want: metav1.ConditionUnknown, message: "Additional Gateway readiness is pending: alpha, beta"},
		{name: "unknown", statuses: []serviceApi.AdditionalIngressStatus{
			gatewayStatus("alpha", metav1.ConditionTrue, 5), gatewayStatus("beta", metav1.ConditionUnknown, 5),
		}, want: metav1.ConditionUnknown, message: "Additional Gateway readiness is pending: beta"},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			config := &serviceApi.GatewayConfig{
				ObjectMeta: metav1.ObjectMeta{Generation: 5},
				Spec:       serviceApi.GatewayConfigSpec{AdditionalIngresses: serviceApi.AdditionalIngresses{{Name: "alpha"}, {Name: "beta"}}},
				Status:     serviceApi.GatewayConfigStatus{AdditionalIngresses: test.statuses},
			}
			rr := &odhtypes.ReconciliationRequest{Instance: config,
				Conditions: conditions.NewManager(config, status.ConditionTypeReady, ReadyConditionType, serviceApi.AdditionalGatewaysReadyConditionType)}
			rr.Conditions.MarkTrue(ReadyConditionType)
			updateAdditionalGatewaysReadyCondition(rr, config)
			condition := rr.Conditions.GetCondition(serviceApi.AdditionalGatewaysReadyConditionType)
			g.Expect(condition.Status).To(Equal(test.want))
			g.Expect(condition.Message).To(Equal(test.message))
			g.Expect(condition.ObservedGeneration).To(Equal(config.Generation))
			g.Expect(rr.Conditions.GetTopLevelCondition().Status).To(Equal(test.want))
			// A ready aggregate must not clear another dependency's failure.
			rr.Conditions.MarkFalse(ReadyConditionType, conditions.WithReason("DefaultGatewayNotReady"))
			config.Spec.AdditionalIngresses = nil
			updateAdditionalGatewaysReadyCondition(rr, config)
			g.Expect(rr.Conditions.GetCondition(serviceApi.AdditionalGatewaysReadyConditionType).Status).To(Equal(metav1.ConditionTrue))
			g.Expect(rr.Conditions.IsHappy()).To(BeFalse())
		})
	}
}
