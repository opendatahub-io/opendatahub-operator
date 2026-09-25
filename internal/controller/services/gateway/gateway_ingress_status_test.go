//nolint:testpackage
package gateway

import (
	"context"
	stderrors "errors"
	"slices"
	"testing"
	"time"

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
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func TestBuildAdditionalIngressStatuses(t *testing.T) {
	g := NewWithT(t)

	statuses := buildAdditionalIngressStatuses(
		[]serviceApi.AdditionalIngress{
			{Name: "zeta", Hostname: "zeta.example.com"},
			{Name: "alpha", Hostname: "alpha.example.com"},
		},
		nil,
		4,
	)

	g.Expect(statuses).To(HaveLen(2))
	g.Expect(statuses[0].Name).To(Equal("zeta"))
	g.Expect(statuses[1].Name).To(Equal("alpha"))
	for _, status := range statuses {
		g.Expect(status.Conditions).To(HaveLen(4))
		for _, condition := range status.Conditions {
			g.Expect(condition.Status).To(Equal(metav1.ConditionUnknown))
			g.Expect(condition.ObservedGeneration).To(Equal(int64(4)))
			g.Expect(condition.Reason).To(Equal(serviceApi.AdditionalIngressReconciliationPendingReason))
		}
	}
}

func TestAdditionalIngressRouteFailureUpdatesStatusBeforePostStatusHook(t *testing.T) {
	g := NewWithT(t)
	ingress := additionalIngress("alpha", "alpha.apps.example.com", 9443, "alpha-shard")
	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{Generation: 5},
		Spec:       serviceApi.GatewayConfigSpec{AdditionalIngresses: serviceApi.AdditionalIngresses{ingress}},
	}
	gatewayConfig.Status.AdditionalIngresses = buildAdditionalIngressStatuses(
		gatewayConfig.Spec.AdditionalIngresses, nil, gatewayConfig.Generation)
	status := additionalIngressStatusByName(gatewayConfig, ingress.Name)
	setAdditionalIngressCondition(status, gatewayConfig.Generation,
		serviceApi.AdditionalIngressRouteAdmittedConditionType, metav1.ConditionTrue,
		additionalIngressReasonReady, "Previously admitted")
	rr := &odhtypes.ReconciliationRequest{Instance: gatewayConfig}

	rejectAdditionalIngressRoute(rr, ingress.Name,
		additionalIngressReasonDependencyUnavailable, "Target IngressController was not found")
	g.Expect(conditions.FindStatusCondition(status, serviceApi.AdditionalIngressRouteAdmittedConditionType).Status).
		To(Equal(metav1.ConditionFalse))
	g.Expect(syncAdditionalIngressReadyStatuses(t.Context(), rr, false)).To(Succeed())
	g.Expect(conditions.FindStatusCondition(status, serviceApi.AdditionalIngressReadyConditionType).Status).
		To(Equal(metav1.ConditionFalse))
}

func TestBuildAdditionalIngressStatusesPreservesCurrentConditions(t *testing.T) {
	g := NewWithT(t)
	ready := common.Condition{
		Type:               serviceApi.AdditionalIngressListenerReadyConditionType,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: 7,
		Reason:             "Programmed",
	}

	statuses := buildAdditionalIngressStatuses(
		[]serviceApi.AdditionalIngress{{Name: "alpha", Hostname: "new.example.com"}},
		[]serviceApi.AdditionalIngressStatus{{
			Name:     "alpha",
			Hostname: "old.example.com",
			Conditions: []common.Condition{
				ready,
				{Type: serviceApi.AdditionalIngressRouteAdmittedConditionType, Status: metav1.ConditionFalse, ObservedGeneration: 6},
			},
		}},
		7,
	)

	g.Expect(statuses).To(HaveLen(1))
	g.Expect(statuses[0].Hostname).To(Equal("new.example.com"))
	g.Expect(statuses[0].Conditions[0]).To(Equal(ready))
	g.Expect(statuses[0].Conditions[1].Status).To(Equal(metav1.ConditionUnknown))
	g.Expect(statuses[0].Conditions[1].ObservedGeneration).To(Equal(int64(7)))
}

func TestBuildAdditionalIngressStatusesAssignsNewTransitionTimeAfterTrue(t *testing.T) {
	g := NewWithT(t)
	previousTransitionTime := metav1.NewTime(time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC))

	statuses := buildAdditionalIngressStatuses(
		[]serviceApi.AdditionalIngress{{Name: "alpha", Hostname: "alpha.example.com"}},
		[]serviceApi.AdditionalIngressStatus{{
			Name: "alpha",
			Conditions: []common.Condition{{
				Type:               serviceApi.AdditionalIngressListenerReadyConditionType,
				Status:             metav1.ConditionTrue,
				ObservedGeneration: 7,
				LastTransitionTime: previousTransitionTime,
			}},
		}},
		8,
	)

	condition := statuses[0].Conditions[0]
	g.Expect(condition.Status).To(Equal(metav1.ConditionUnknown))
	g.Expect(condition.ObservedGeneration).To(Equal(int64(8)))
	g.Expect(condition.LastTransitionTime.IsZero()).To(BeFalse())
	g.Expect(condition.LastTransitionTime).NotTo(Equal(previousTransitionTime))
}

func TestBuildAdditionalIngressStatusesPrunesRemovedEntries(t *testing.T) {
	g := NewWithT(t)

	statuses := buildAdditionalIngressStatuses(
		[]serviceApi.AdditionalIngress{{Name: "current", Hostname: "current.example.com"}},
		[]serviceApi.AdditionalIngressStatus{
			{Name: "current"},
			{Name: "removed"},
		},
		2,
	)

	g.Expect(statuses).To(HaveLen(1))
	g.Expect(statuses[0].Name).To(Equal("current"))
}

func TestAdditionalIngressConditionUsesFrameworkTransitionTime(t *testing.T) {
	g := NewWithT(t)
	previous := metav1.NewTime(time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC))
	status := &serviceApi.AdditionalIngressStatus{Conditions: []common.Condition{{
		Type: serviceApi.AdditionalIngressRouteAdmittedConditionType, Status: metav1.ConditionFalse,
		ObservedGeneration: 5, LastTransitionTime: previous, Reason: "NotAdmitted", Message: "Waiting",
	}}}

	setAdditionalIngressCondition(status, 5, serviceApi.AdditionalIngressRouteAdmittedConditionType,
		metav1.ConditionFalse, "NotAdmitted", "Waiting")
	g.Expect(status.Conditions[0].LastTransitionTime).To(Equal(previous))
	setAdditionalIngressCondition(status, 5, serviceApi.AdditionalIngressRouteAdmittedConditionType,
		metav1.ConditionFalse, "StillNotAdmitted", "Still waiting")
	g.Expect(status.Conditions[0].LastTransitionTime).NotTo(Equal(previous))
}

func TestAuthenticationStatusStaysUnknownUntilConfigured(t *testing.T) {
	g := NewWithT(t)
	status := &serviceApi.AdditionalIngressStatus{Conditions: []common.Condition{{
		Type:   serviceApi.AdditionalIngressAuthenticationReadyConditionType,
		Status: metav1.ConditionTrue, ObservedGeneration: 5, Reason: additionalIngressReasonReady,
	}}}

	markAuthenticationStatusUnavailable(status, 5)
	condition := conditions.FindStatusCondition(status, serviceApi.AdditionalIngressAuthenticationReadyConditionType)
	g.Expect(condition.Status).To(Equal(metav1.ConditionUnknown))
	g.Expect(condition.Reason).To(Equal(additionalIngressReasonStatusUnavailable))
}

func TestSyncAdditionalIngressReadinessPublishesPerIngressConditions(t *testing.T) {
	g := NewWithT(t)
	alpha := additionalIngress("alpha", "alpha.apps.example.com", 9443, "alpha-shard")
	beta := additionalIngress("beta", "beta.apps.example.com", 9444, "missing-shard")
	ingresses := serviceApi.AdditionalIngresses{alpha, beta}
	gatewayConfig := &serviceApi.GatewayConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceApi.GatewayConfigName, UID: types.UID("gateway-config-uid"), Generation: 5,
		},
		Spec: serviceApi.GatewayConfigSpec{AdditionalIngresses: ingresses},
	}
	gatewayConfig.Status.AdditionalIngresses = buildAdditionalIngressStatuses(ingresses, nil, gatewayConfig.Generation)

	gateway := &gwapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: GetDefaultGatewayName(), Namespace: GetGatewayNamespace(), Generation: 3},
		Spec: gwapiv1.GatewaySpec{Listeners: []gwapiv1.Listener{
			{Name: gwapiv1.SectionName(alpha.Name)},
			{Name: gwapiv1.SectionName(beta.Name)},
		}},
		Status: gwapiv1.GatewayStatus{Listeners: []gwapiv1.ListenerStatus{
			readyAdditionalIngressListener(alpha.Name),
			readyAdditionalIngressListener(beta.Name),
		}},
	}
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{Name: GetAdditionalIngressRouteName(alpha.Name), Namespace: GetGatewayNamespace()},
		Spec: routev1.RouteSpec{
			Host: alpha.Hostname,
			To:   routev1.RouteTargetReference{Name: "gateway-provider"},
			Port: &routev1.RoutePort{TargetPort: intstr.FromInt(int(alpha.ListenerPort))},
		},
		Status: routev1.RouteStatus{Ingress: []routev1.RouteIngress{{
			Host:       alpha.Hostname,
			RouterName: alpha.IngressControllerName,
			Conditions: []routev1.RouteIngressCondition{{Type: routev1.RouteAdmitted, Status: corev1.ConditionTrue}},
		}}},
	}
	setGatewayConfigOwner(route, gatewayConfig)
	cli := newGatewayTestClient(t, gatewayConfig, gateway, route)
	rr := &odhtypes.ReconciliationRequest{Client: cli, Instance: gatewayConfig,
		Extensions: map[string]any{additionalIngressRouteServicesKey: map[string]string{
			alpha.Name: "gateway-provider",
		}},
	}
	recordAdditionalIngressRouteCondition(rr, beta.Name, metav1.ConditionFalse,
		additionalIngressReasonDependencyUnavailable, "Target IngressController was not found")

	g.Expect(syncAdditionalIngressReadiness(t.Context(), rr)).To(Succeed())
	g.Expect(syncAdditionalIngressReadyStatuses(t.Context(), rr, false)).To(Succeed())
	expectIngressCondition := func(name, conditionType string, status metav1.ConditionStatus, reason string) {
		t.Helper()
		ingressStatus := additionalIngressStatusByName(gatewayConfig, name)
		condition := conditions.FindStatusCondition(ingressStatus, conditionType)
		g.Expect(condition).NotTo(BeNil())
		g.Expect(condition.Status).To(Equal(status))
		g.Expect(condition.ObservedGeneration).To(Equal(int64(5)))
		g.Expect(condition.Reason).To(Equal(reason))
		g.Expect(condition.Message).NotTo(BeEmpty())
	}
	expectIngressCondition(alpha.Name, serviceApi.AdditionalIngressListenerReadyConditionType, metav1.ConditionTrue,
		additionalIngressReasonReady)
	expectIngressCondition(alpha.Name, serviceApi.AdditionalIngressRouteAdmittedConditionType, metav1.ConditionTrue,
		additionalIngressReasonReady)
	expectIngressCondition(alpha.Name, serviceApi.AdditionalIngressAuthenticationReadyConditionType, metav1.ConditionUnknown,
		additionalIngressReasonStatusUnavailable)
	expectIngressCondition(alpha.Name, serviceApi.AdditionalIngressReadyConditionType, metav1.ConditionUnknown,
		additionalIngressReasonStatusUnavailable)
	expectIngressCondition(beta.Name, serviceApi.AdditionalIngressListenerReadyConditionType, metav1.ConditionTrue,
		additionalIngressReasonReady)
	expectIngressCondition(beta.Name, serviceApi.AdditionalIngressRouteAdmittedConditionType, metav1.ConditionFalse,
		additionalIngressReasonDependencyUnavailable)
	expectIngressCondition(beta.Name, serviceApi.AdditionalIngressReadyConditionType, metav1.ConditionFalse,
		additionalIngressReasonDependencyUnavailable)
	alphaStatus := additionalIngressStatusByName(gatewayConfig, alpha.Name)
	previousConditions := slices.Clone(alphaStatus.Conditions)
	g.Expect(syncAdditionalIngressReadiness(t.Context(), rr)).To(Succeed())
	g.Expect(syncAdditionalIngressReadyStatuses(t.Context(), rr, false)).To(Succeed())
	g.Expect(alphaStatus.Conditions).To(Equal(previousConditions))
}

func TestAdditionalIngressListenerCondition(t *testing.T) {
	for _, test := range []struct {
		name              string
		gatewayGeneration int64
		programmedStatus  metav1.ConditionStatus
		wantMessage       string
	}{
		{
			name:              "requires current Gateway generation",
			gatewayGeneration: 4,
			programmedStatus:  metav1.ConditionTrue,
			wantMessage:       `Gateway listener "alpha" status has not observed the current Gateway generation`,
		},
		{
			name:              "handles unknown status without reason",
			gatewayGeneration: 3,
			programmedStatus:  metav1.ConditionUnknown,
			wantMessage:       `Gateway listener "alpha" has not reported Programmed as ready`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			listener := readyAdditionalIngressListener("alpha")
			listener.Conditions[2].Status = test.programmedStatus
			gateway := &gwapiv1.Gateway{
				ObjectMeta: metav1.ObjectMeta{Generation: test.gatewayGeneration},
				Spec:       gwapiv1.GatewaySpec{Listeners: []gwapiv1.Listener{{Name: "alpha"}}},
				Status:     gwapiv1.GatewayStatus{Listeners: []gwapiv1.ListenerStatus{listener}},
			}

			condition := additionalIngressListenerCondition(gateway, "alpha", 9)
			g.Expect(condition.Status).To(Equal(metav1.ConditionUnknown))
			g.Expect(condition.Reason).To(Equal(serviceApi.AdditionalIngressReconciliationPendingReason))
			g.Expect(condition.ObservedGeneration).To(Equal(int64(9)))
			g.Expect(condition.Message).To(Equal(test.wantMessage))
		})
	}
}

func TestAdditionalIngressRouteCondition(t *testing.T) {
	ingress := additionalIngress("alpha", "new.apps.example.com", 9443, "alpha-shard")
	for _, test := range []struct {
		name         string
		route        routev1.RouteStatus
		wantStatus   metav1.ConditionStatus
		wantReason   string
		wantMessage  string
		wantSeverity common.ConditionSeverity
	}{
		{
			name: "reports informational overlap when multiple IngressControllers admit the Route",
			route: routev1.RouteStatus{Ingress: []routev1.RouteIngress{
				{Host: ingress.Hostname, RouterName: ingress.IngressControllerName, Conditions: []routev1.RouteIngressCondition{{
					Type: routev1.RouteAdmitted, Status: corev1.ConditionTrue,
				}}},
				{Host: ingress.Hostname, RouterName: "sibling-shard", Conditions: []routev1.RouteIngressCondition{{
					Type: routev1.RouteAdmitted, Status: corev1.ConditionTrue,
				}}},
				{Host: ingress.Hostname, RouterName: "default", Conditions: []routev1.RouteIngressCondition{{
					Type: routev1.RouteAdmitted, Status: corev1.ConditionTrue,
				}}},
			}},
			wantStatus:   metav1.ConditionTrue,
			wantReason:   additionalIngressReasonMultipleControllers,
			wantSeverity: common.ConditionSeverityInfo,
			wantMessage: "IngressController \"alpha-shard\" admitted the bridge Route; " +
				"bridge Route is admitted by multiple IngressControllers: alpha-shard, default, sibling-shard",
		},
		{
			name: "does not warn for a single admission",
			route: routev1.RouteStatus{Ingress: []routev1.RouteIngress{
				{Host: ingress.Hostname, RouterName: ingress.IngressControllerName, Conditions: []routev1.RouteIngressCondition{{
					Type: routev1.RouteAdmitted, Status: corev1.ConditionTrue,
				}}},
				{Host: ingress.Hostname, RouterName: "sibling-shard", Conditions: []routev1.RouteIngressCondition{{
					Type: routev1.RouteAdmitted, Status: corev1.ConditionFalse,
				}}},
				{Host: "old.apps.example.com", RouterName: "default", Conditions: []routev1.RouteIngressCondition{{
					Type: routev1.RouteAdmitted, Status: corev1.ConditionTrue,
				}}},
			}},
			wantStatus:  metav1.ConditionTrue,
			wantReason:  additionalIngressReasonReady,
			wantMessage: "IngressController \"alpha-shard\" admitted the bridge Route",
		},
		{
			name: "waits for target despite another IngressController admission",
			route: routev1.RouteStatus{Ingress: []routev1.RouteIngress{{
				Host: ingress.Hostname, RouterName: "sibling-shard",
				Conditions: []routev1.RouteIngressCondition{{Type: routev1.RouteAdmitted, Status: corev1.ConditionTrue}},
			}}},
			wantStatus: metav1.ConditionUnknown,
			wantReason: serviceApi.AdditionalIngressReconciliationPendingReason,
		},
		{
			name: "keeps pending status when other IngressControllers overlap",
			route: routev1.RouteStatus{Ingress: []routev1.RouteIngress{
				{Host: ingress.Hostname, RouterName: "sibling-shard", Conditions: []routev1.RouteIngressCondition{{
					Type: routev1.RouteAdmitted, Status: corev1.ConditionTrue,
				}}},
				{Host: ingress.Hostname, RouterName: "default", Conditions: []routev1.RouteIngressCondition{{
					Type: routev1.RouteAdmitted, Status: corev1.ConditionTrue,
				}}},
			}},
			wantStatus:   metav1.ConditionUnknown,
			wantReason:   serviceApi.AdditionalIngressReconciliationPendingReason,
			wantSeverity: common.ConditionSeverityError,
			wantMessage:  "Waiting for IngressController \"alpha-shard\" to report Route admission; bridge Route is admitted by multiple IngressControllers: default, sibling-shard",
		},
		{
			name: "keeps failure severity when other IngressControllers overlap",
			route: routev1.RouteStatus{Ingress: []routev1.RouteIngress{
				{Host: ingress.Hostname, RouterName: ingress.IngressControllerName, Conditions: []routev1.RouteIngressCondition{{
					Type: routev1.RouteAdmitted, Status: corev1.ConditionFalse,
				}}},
				{Host: ingress.Hostname, RouterName: "sibling-shard", Conditions: []routev1.RouteIngressCondition{{
					Type: routev1.RouteAdmitted, Status: corev1.ConditionTrue,
				}}},
				{Host: ingress.Hostname, RouterName: "default", Conditions: []routev1.RouteIngressCondition{{
					Type: routev1.RouteAdmitted, Status: corev1.ConditionTrue,
				}}},
			}},
			wantStatus:   metav1.ConditionFalse,
			wantReason:   additionalIngressReasonNotReady,
			wantSeverity: common.ConditionSeverityError,
		},
		{
			name: "ignores old host admission",
			route: routev1.RouteStatus{Ingress: []routev1.RouteIngress{{
				Host: "old.apps.example.com", RouterName: ingress.IngressControllerName,
				Conditions: []routev1.RouteIngressCondition{{Type: routev1.RouteAdmitted, Status: corev1.ConditionTrue}},
			}}},
			wantStatus: metav1.ConditionUnknown,
			wantReason: serviceApi.AdditionalIngressReconciliationPendingReason,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			condition := additionalIngressRouteCondition(&routev1.Route{Status: test.route}, ingress, 2)
			g.Expect(condition.Status).To(Equal(test.wantStatus))
			g.Expect(condition.Reason).To(Equal(test.wantReason))
			g.Expect(condition.Severity).To(Equal(test.wantSeverity))
			g.Expect(condition.ObservedGeneration).To(Equal(int64(2)))
			if test.wantMessage != "" {
				g.Expect(condition.Message).To(Equal(test.wantMessage))
			}
			if condition.Reason == additionalIngressReasonMultipleControllers {
				status := &serviceApi.AdditionalIngressStatus{Conditions: []common.Condition{
					condition,
					{Type: serviceApi.AdditionalIngressListenerReadyConditionType, Status: metav1.ConditionTrue, ObservedGeneration: 2},
					{Type: serviceApi.AdditionalIngressAuthenticationReadyConditionType, Status: metav1.ConditionTrue, ObservedGeneration: 2},
				}}
				updateAdditionalIngressReadyCondition(status, 2)
				g.Expect(conditions.FindStatusCondition(status, serviceApi.AdditionalIngressReadyConditionType).Status).
					To(Equal(metav1.ConditionTrue))
			}
		})
	}
}

func TestSyncAdditionalIngressRouteReadinessRejectsStaleDestination(t *testing.T) {
	ingress := additionalIngress("alpha", "alpha.apps.example.com", 9443, "alpha-shard")
	for _, test := range []struct {
		name        string
		serviceName string
		port        int
	}{
		{name: "Service changed", serviceName: "old-provider", port: 9443},
		{name: "port changed", serviceName: "gateway-provider", port: 8443},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)
			gatewayConfig := &serviceApi.GatewayConfig{ObjectMeta: metav1.ObjectMeta{
				Name: serviceApi.GatewayConfigName, UID: types.UID("gateway-config-uid"), Generation: 5,
			}}
			route := &routev1.Route{
				ObjectMeta: metav1.ObjectMeta{Name: GetAdditionalIngressRouteName(ingress.Name), Namespace: GetGatewayNamespace()},
				Spec: routev1.RouteSpec{
					Host: ingress.Hostname, To: routev1.RouteTargetReference{Name: test.serviceName},
					Port: &routev1.RoutePort{TargetPort: intstr.FromInt(test.port)},
				},
			}
			setGatewayConfigOwner(route, gatewayConfig)
			rr := &odhtypes.ReconciliationRequest{Client: newGatewayTestClient(t, route), Instance: gatewayConfig}
			status := &serviceApi.AdditionalIngressStatus{}

			g.Expect(syncAdditionalIngressRouteReadiness(t.Context(), rr, gatewayConfig, ingress,
				"gateway-provider", status)).To(Succeed())
			condition := conditions.FindStatusCondition(status, serviceApi.AdditionalIngressRouteAdmittedConditionType)
			g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			g.Expect(condition.Reason).To(Equal(additionalIngressReasonNotReady))
		})
	}
}

func TestSyncAdditionalIngressReadinessRetriesReadFailures(t *testing.T) {
	g := NewWithT(t)
	ingress := additionalIngress("alpha", "alpha.apps.example.com", 9443, "alpha-shard")
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
		Extensions: map[string]any{additionalIngressRouteServicesKey: map[string]string{
			ingress.Name: "gateway-provider",
		}},
	}

	err = syncAdditionalIngressReadiness(t.Context(), rr)
	var requeue odherrors.RequeueAfterError
	g.Expect(stderrors.As(err, &requeue)).To(BeTrue())
	status := additionalIngressStatusByName(gatewayConfig, ingress.Name)
	g.Expect(conditions.FindStatusCondition(status, serviceApi.AdditionalIngressListenerReadyConditionType).Reason).
		To(Equal(additionalIngressReasonStatusReadFailed))
	g.Expect(conditions.FindStatusCondition(status, serviceApi.AdditionalIngressRouteAdmittedConditionType).Reason).
		To(Equal(additionalIngressReasonStatusReadFailed))
}

func readyAdditionalIngressListener(name string) gwapiv1.ListenerStatus {
	conditions := []metav1.Condition{
		{Type: string(gwapiv1.ListenerConditionAccepted), Status: metav1.ConditionTrue, ObservedGeneration: 3},
		{Type: string(gwapiv1.ListenerConditionResolvedRefs), Status: metav1.ConditionTrue, ObservedGeneration: 3},
		{Type: string(gwapiv1.ListenerConditionProgrammed), Status: metav1.ConditionTrue, ObservedGeneration: 3},
	}
	return gwapiv1.ListenerStatus{Name: gwapiv1.SectionName(name), Conditions: conditions}
}
