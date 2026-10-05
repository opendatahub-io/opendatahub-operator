package gateway

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const (
	additionalIngressReasonDependencyUnavailable = "DependencyUnavailable"
	additionalIngressReasonOwnershipConflict     = "OwnershipConflict"
	additionalIngressReasonHostnameConflict      = "HostnameConflict"
	additionalIngressReasonStatusReadFailed      = "StatusReadFailed"
	additionalIngressReasonStatusUnavailable     = "StatusUnavailable"
	additionalIngressReasonNotReady              = "NotReady"
	additionalIngressReasonReady                 = "Ready"
	additionalIngressReasonMultipleControllers   = "MultipleIngressControllers"
	additionalIngressReasonNoAdditionalGateways  = "NoAdditionalGateways"
)

var additionalIngressConditionTypes = []string{
	serviceApi.AdditionalIngressGatewayReadyConditionType,
	serviceApi.AdditionalIngressRouteAdmittedConditionType,
	serviceApi.AdditionalIngressAuthenticationReadyConditionType,
	serviceApi.AdditionalIngressReadyConditionType,
}

// syncAdditionalIngressStatus inventories configured ingresses before actions
// report their per-ingress conditions.
func syncAdditionalIngressStatus(_ context.Context, rr *odhtypes.ReconciliationRequest) error {
	gatewayConfig, err := validateGatewayConfig(rr)
	if err != nil {
		return err
	}
	gatewayConfig.Status.AdditionalIngresses = buildAdditionalIngressStatuses(
		gatewayConfig.Spec.AdditionalIngresses,
		gatewayConfig.Status.AdditionalIngresses,
		gatewayConfig.Generation,
	)
	updateAdditionalGatewaysReadyCondition(rr, gatewayConfig)
	return nil
}

// updateAdditionalGatewaysReadyCondition uses the recorded GatewayReady conditions,
// before the reconciler computes overall Ready and phase.
func updateAdditionalGatewaysReadyCondition(rr *odhtypes.ReconciliationRequest, gatewayConfig *serviceApi.GatewayConfig) {
	var failed, pending []string
	for _, ingress := range gatewayConfig.Spec.AdditionalIngresses {
		status := additionalIngressStatusByName(gatewayConfig, ingress.Name)
		var condition *common.Condition
		if status != nil {
			condition = conditions.FindStatusCondition(status, serviceApi.AdditionalIngressGatewayReadyConditionType)
		}
		switch {
		case condition == nil || condition.ObservedGeneration != gatewayConfig.Generation:
			pending = append(pending, ingress.Name)
		case condition.Status == metav1.ConditionFalse:
			failed = append(failed, ingress.Name)
		case condition.Status != metav1.ConditionTrue:
			pending = append(pending, ingress.Name)
		}
	}

	conditionStatus := metav1.ConditionTrue
	reason, message := additionalIngressReasonReady, "All additional Gateways are ready"
	switch {
	case len(failed) > 0:
		conditionStatus = metav1.ConditionFalse
		reason, message = additionalIngressReasonNotReady, "Additional Gateways are not ready: "+strings.Join(failed, ", ")
	case len(pending) > 0:
		conditionStatus = metav1.ConditionUnknown
		reason, message = serviceApi.AdditionalIngressReconciliationPendingReason, "Additional Gateway readiness is pending: "+strings.Join(pending, ", ")
	case len(gatewayConfig.Spec.AdditionalIngresses) == 0:
		reason, message = additionalIngressReasonNoAdditionalGateways, "No additional Gateways are configured"
	}
	rr.Conditions.Mark(serviceApi.AdditionalGatewaysReadyConditionType, conditionStatus,
		conditions.WithReason(reason), conditions.WithMessage(message),
		conditions.WithObservedGeneration(gatewayConfig.Generation),
		conditions.WithSeverity(common.ConditionSeverityError))
}

func recordAdditionalIngressRouteCondition(
	rr *odhtypes.ReconciliationRequest,
	ingressName string,
	conditionStatus metav1.ConditionStatus,
	reason, message string,
) {
	gatewayConfig, ok := rr.Instance.(*serviceApi.GatewayConfig)
	if !ok {
		return
	}
	if status := additionalIngressStatusByName(gatewayConfig, ingressName); status != nil {
		setAdditionalIngressCondition(status, gatewayConfig.Generation,
			serviceApi.AdditionalIngressRouteAdmittedConditionType, conditionStatus, reason, message)
	}
}

func recordAllAdditionalIngressRouteConditions(
	rr *odhtypes.ReconciliationRequest,
	ingresses serviceApi.AdditionalIngresses,
	reason, message string,
) {
	for _, ingress := range ingresses {
		recordAdditionalIngressRouteCondition(rr, ingress.Name, metav1.ConditionUnknown, reason, message)
	}
}

// syncAdditionalIngressReadyStatuses derives per-ingress readiness after all
// reconciliation actions have reported their conditions, including on errors.
func syncAdditionalIngressReadyStatuses(_ context.Context, rr *odhtypes.ReconciliationRequest, _ bool) error {
	gatewayConfig, err := validateGatewayConfig(rr)
	if err != nil {
		return err
	}
	for i := range gatewayConfig.Status.AdditionalIngresses {
		updateAdditionalIngressReadyCondition(&gatewayConfig.Status.AdditionalIngresses[i], gatewayConfig.Generation)
	}
	return nil
}

// syncAdditionalIngressReadiness publishes readiness from the live Gateway and
// bridge Route status. AuthenticationReady remains Unknown until an
// ingress-specific authentication status source is available.
func syncAdditionalIngressReadiness(ctx context.Context, rr *odhtypes.ReconciliationRequest) error {
	gatewayConfig, err := validateGatewayConfig(rr)
	if err != nil {
		return err
	}
	defer updateAdditionalGatewaysReadyCondition(rr, gatewayConfig)
	if len(gatewayConfig.Spec.AdditionalIngresses) == 0 {
		return nil
	}

	l := logf.FromContext(ctx).WithName("syncAdditionalIngressReadiness")
	retry := false

	for _, ingress := range gatewayConfig.Spec.AdditionalIngresses {
		status := additionalIngressStatusByName(gatewayConfig, ingress.Name)
		if status == nil || additionalIngressHasGatewayConflict(gatewayConfig, ingress.Name) {
			continue
		}

		gatewayName := ingress.Name
		gateway := &gwapiv1.Gateway{}
		gatewayErr := rr.Client.Get(ctx, types.NamespacedName{
			Name: gatewayName, Namespace: GetGatewayNamespace(),
		}, gateway)
		switch {
		case k8serr.IsNotFound(gatewayErr):
			setAdditionalIngressCondition(status, gatewayConfig.Generation,
				serviceApi.AdditionalIngressGatewayReadyConditionType, metav1.ConditionUnknown,
				serviceApi.AdditionalIngressReconciliationPendingReason,
				fmt.Sprintf("Gateway %q has not been observed yet", gatewayName))
		case gatewayErr != nil:
			l.Error(gatewayErr, "Failed to read additional Gateway status", "gateway", gatewayName)
			setAdditionalIngressCondition(status, gatewayConfig.Generation,
				serviceApi.AdditionalIngressGatewayReadyConditionType, metav1.ConditionUnknown,
				additionalIngressReasonStatusReadFailed, "The additional Gateway status could not be read")
			retry = true
		default:
			conditionStatus := metav1.ConditionFalse
			reason, message := additionalIngressReasonNotReady, fmt.Sprintf("Gateway %q is not accepted", gatewayName)
			if isGatewayReady(gateway) {
				conditionStatus = metav1.ConditionTrue
				reason, message = additionalIngressReasonReady, fmt.Sprintf("Gateway %q is accepted", gatewayName)
			}
			setAdditionalIngressCondition(status, gatewayConfig.Generation,
				serviceApi.AdditionalIngressGatewayReadyConditionType, conditionStatus, reason, message)
		}

		if err := syncAdditionalIngressRouteReadiness(ctx, rr, gatewayConfig, ingress, status); err != nil {
			l.Error(err, "Failed to read additional ingress Route status", "ingress", ingress.Name)
			retry = true
		}
		// TODO: Derive this condition from per-ingress auth status once additional-ingress auth is configured.
		markAuthenticationStatusUnavailable(status, gatewayConfig.Generation)
	}

	if retry {
		return errors.NewRequeueAfterError(30 * time.Second)
	}
	return nil
}

func routeHasLabels(actual, required map[string]string) bool {
	for key, value := range required {
		if actualValue, found := actual[key]; !found || actualValue != value {
			return false
		}
	}
	return true
}

func syncAdditionalIngressRouteReadiness(
	ctx context.Context,
	rr *odhtypes.ReconciliationRequest,
	gatewayConfig *serviceApi.GatewayConfig,
	ingress serviceApi.AdditionalIngress,
	ingressStatus *serviceApi.AdditionalIngressStatus,
) error {
	serviceName := GetGatewayServiceFullName(ingress.Name)

	route := &routev1.Route{}
	routeKey := client.ObjectKey{
		Name: ingress.Name, Namespace: GetGatewayNamespace(),
	}
	err := rr.Client.Get(ctx, routeKey, route)
	if k8serr.IsNotFound(err) || meta.IsNoMatchError(err) {
		setAdditionalIngressCondition(ingressStatus, gatewayConfig.Generation,
			serviceApi.AdditionalIngressRouteAdmittedConditionType, metav1.ConditionUnknown,
			serviceApi.AdditionalIngressReconciliationPendingReason, "The bridge Route has not been observed yet")
		return nil
	}
	if err != nil {
		setAdditionalIngressCondition(ingressStatus, gatewayConfig.Generation,
			serviceApi.AdditionalIngressRouteAdmittedConditionType, metav1.ConditionUnknown,
			additionalIngressReasonStatusReadFailed, "The bridge Route status could not be read")
		return fmt.Errorf("failed to get additional ingress Route %q: %w", routeKey.Name, err)
	}
	if !isOwnedByGatewayConfig(route, gatewayConfig) {
		setAdditionalIngressCondition(ingressStatus, gatewayConfig.Generation,
			serviceApi.AdditionalIngressRouteAdmittedConditionType, metav1.ConditionFalse,
			additionalIngressReasonOwnershipConflict,
			fmt.Sprintf("Route %q is not controlled by this GatewayConfig", route.Name))
		return nil
	}
	if route.Spec.Host != ingress.Hostname || route.Spec.To.Name != serviceName ||
		route.Spec.Port == nil || route.Spec.Port.TargetPort != intstr.FromInt(StandardHTTPSPort) ||
		!routeHasLabels(route.Labels, gatewayRouteLabels(ingress.RouteLabels)) {
		setAdditionalIngressCondition(ingressStatus, gatewayConfig.Generation,
			serviceApi.AdditionalIngressRouteAdmittedConditionType, metav1.ConditionFalse,
			additionalIngressReasonNotReady,
			fmt.Sprintf("Route %q does not match the configured hostname, Service, HTTPS port, or labels", route.Name))
		return nil
	}

	conditions.SetStatusCondition(ingressStatus, additionalIngressRouteCondition(route, ingress, gatewayConfig.Generation))
	return nil
}

func additionalIngressRouteCondition(
	route *routev1.Route,
	ingress serviceApi.AdditionalIngress,
	generation int64,
) common.Condition {
	var targetAdmission *routev1.RouteIngressCondition
	admittedRouters := make(map[string]struct{})
	for _, routeIngress := range route.Status.Ingress {
		if routeIngress.Host != ingress.Hostname {
			continue
		}
		for i := range routeIngress.Conditions {
			routeCondition := &routeIngress.Conditions[i]
			if routeCondition.Type != routev1.RouteAdmitted {
				continue
			}
			if routeCondition.Status == corev1.ConditionTrue && routeIngress.RouterName != "" {
				admittedRouters[routeIngress.RouterName] = struct{}{}
			}
			if routeIngress.RouterName != ingress.IngressControllerName {
				continue
			}
			if targetAdmission == nil || routeCondition.Status == corev1.ConditionTrue {
				targetAdmission = routeCondition
			}
		}
	}
	var status metav1.ConditionStatus
	var reason, message string
	switch {
	case targetAdmission == nil:
		status = metav1.ConditionFalse
		reason = additionalIngressReasonNotReady
		message = fmt.Sprintf("IngressController %q has not admitted the bridge Route", ingress.IngressControllerName)
	case targetAdmission.Status == corev1.ConditionTrue:
		status = metav1.ConditionTrue
		reason = additionalIngressReasonReady
		message = fmt.Sprintf("IngressController %q admitted the bridge Route", ingress.IngressControllerName)
	case targetAdmission.Status == corev1.ConditionFalse:
		status = metav1.ConditionFalse
		reason = additionalIngressReasonNotReady
		message = fmt.Sprintf("IngressController %q has not admitted the bridge Route", ingress.IngressControllerName)
		if details := conditionDetails(targetAdmission.Reason, targetAdmission.Message); details != "" {
			message += ": " + details
		}
	default:
		status = metav1.ConditionUnknown
		reason = serviceApi.AdditionalIngressReconciliationPendingReason
		message = fmt.Sprintf("Waiting for IngressController %q to report Route admission", ingress.IngressControllerName)
		if details := conditionDetails(targetAdmission.Reason, targetAdmission.Message); details != "" {
			message += ": " + details
		}
	}

	severity := common.ConditionSeverityError
	if len(admittedRouters) > 1 {
		routers := make([]string, 0, len(admittedRouters))
		for router := range admittedRouters {
			routers = append(routers, router)
		}
		slices.Sort(routers)
		message += "; bridge Route is admitted by multiple IngressControllers: " + strings.Join(routers, ", ")
		if status == metav1.ConditionTrue {
			reason = additionalIngressReasonMultipleControllers
			severity = common.ConditionSeverityInfo
		}
	}
	return common.Condition{
		Type:               serviceApi.AdditionalIngressRouteAdmittedConditionType,
		Status:             status,
		ObservedGeneration: generation,
		Reason:             reason,
		Message:            message,
		Severity:           severity,
	}
}

func markAuthenticationStatusUnavailable(status *serviceApi.AdditionalIngressStatus, generation int64) {
	setAdditionalIngressCondition(status, generation,
		serviceApi.AdditionalIngressAuthenticationReadyConditionType, metav1.ConditionUnknown,
		additionalIngressReasonStatusUnavailable,
		"Per-ingress authentication readiness is not reported by the current auth resources")
}

func updateAdditionalIngressReadyCondition(status *serviceApi.AdditionalIngressStatus, generation int64) {
	conditionTypes := []string{
		serviceApi.AdditionalIngressGatewayReadyConditionType,
		serviceApi.AdditionalIngressRouteAdmittedConditionType,
	}
	var failed, pending *common.Condition
	for _, conditionType := range conditionTypes {
		condition := conditions.FindStatusCondition(status, conditionType)
		if condition == nil || condition.ObservedGeneration != generation {
			if pending == nil {
				pending = &common.Condition{Type: conditionType, Status: metav1.ConditionUnknown,
					Reason:  serviceApi.AdditionalIngressReconciliationPendingReason,
					Message: "Condition has not observed the current GatewayConfig generation"}
			}
			continue
		}
		if condition.Status == metav1.ConditionFalse && failed == nil {
			failed = condition
		}
		if condition.Status != metav1.ConditionTrue && pending == nil {
			pending = condition
		}
	}

	switch {
	case failed != nil:
		message := failed.Message
		if message == "" {
			message = failed.Reason
		}
		setAdditionalIngressCondition(status, generation, serviceApi.AdditionalIngressReadyConditionType,
			metav1.ConditionFalse, firstNonEmpty(failed.Reason, additionalIngressReasonNotReady),
			fmt.Sprintf("%s: %s", failed.Type, message))
	case pending != nil:
		setAdditionalIngressCondition(status, generation, serviceApi.AdditionalIngressReadyConditionType,
			metav1.ConditionUnknown, firstNonEmpty(pending.Reason, serviceApi.AdditionalIngressReconciliationPendingReason),
			fmt.Sprintf("%s: %s", pending.Type, pending.Message))
	default:
		setAdditionalIngressCondition(status, generation, serviceApi.AdditionalIngressReadyConditionType,
			metav1.ConditionTrue, additionalIngressReasonReady,
			"Gateway and Route are ready")
	}
}

func setAdditionalIngressCondition(
	status *serviceApi.AdditionalIngressStatus,
	generation int64,
	conditionType string,
	conditionStatus metav1.ConditionStatus,
	reason string,
	message string,
) {
	conditions.SetStatusCondition(status, common.Condition{
		Type:               conditionType,
		Status:             conditionStatus,
		ObservedGeneration: generation,
		Reason:             reason,
		Message:            message,
	})
}

func additionalIngressStatusByName(
	gatewayConfig *serviceApi.GatewayConfig,
	name string,
) *serviceApi.AdditionalIngressStatus {
	if gatewayConfig == nil {
		return nil
	}
	for i := range gatewayConfig.Status.AdditionalIngresses {
		if gatewayConfig.Status.AdditionalIngresses[i].Name == name {
			return &gatewayConfig.Status.AdditionalIngresses[i]
		}
	}
	return nil
}

func additionalIngressHasGatewayConflict(gatewayConfig *serviceApi.GatewayConfig, name string) bool {
	status := additionalIngressStatusByName(gatewayConfig, name)
	if status == nil {
		return false
	}
	condition := conditions.FindStatusCondition(status, serviceApi.AdditionalIngressGatewayReadyConditionType)
	return condition != nil && condition.ObservedGeneration == gatewayConfig.Generation &&
		condition.Status == metav1.ConditionFalse &&
		(condition.Reason == additionalIngressReasonOwnershipConflict || condition.Reason == additionalIngressReasonHostnameConflict)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func conditionDetails(reason, message string) string {
	if reason == "" {
		return message
	}
	if message == "" {
		return reason
	}
	return reason + ": " + message
}

func buildAdditionalIngressStatuses(
	specs []serviceApi.AdditionalIngress,
	previous []serviceApi.AdditionalIngressStatus,
	generation int64,
) []serviceApi.AdditionalIngressStatus {
	previousByName := make(map[string]serviceApi.AdditionalIngressStatus, len(previous))
	for _, status := range previous {
		previousByName[status.Name] = status
	}

	statuses := make([]serviceApi.AdditionalIngressStatus, 0, len(specs))
	for _, spec := range specs {
		status := serviceApi.AdditionalIngressStatus{
			Name:     spec.Name,
			Hostname: spec.Hostname,
			GatewayRef: serviceApi.GatewayReference{
				Name: spec.Name, Namespace: GetGatewayNamespace(),
			},
		}
		previousStatus := previousByName[spec.Name]
		if previousStatus.GatewayRef != status.GatewayRef {
			previousStatus.Conditions = nil
		}
		status.Conditions = currentAdditionalIngressConditions(previousStatus.Conditions, generation)
		statuses = append(statuses, status)
	}

	return statuses
}

func currentAdditionalIngressConditions(conditions []common.Condition, generation int64) []common.Condition {
	byType := make(map[string]common.Condition, len(conditions))
	for _, condition := range conditions {
		byType[condition.Type] = condition
	}

	result := make([]common.Condition, 0, len(additionalIngressConditionTypes))
	now := metav1.Now()
	for _, conditionType := range additionalIngressConditionTypes {
		if condition, found := byType[conditionType]; found && condition.ObservedGeneration == generation {
			result = append(result, condition)
			continue
		}
		condition := common.Condition{
			Type:               conditionType,
			Status:             metav1.ConditionUnknown,
			ObservedGeneration: generation,
			LastTransitionTime: now,
			Reason:             serviceApi.AdditionalIngressReconciliationPendingReason,
			Message:            "Ingress readiness has not been observed yet",
		}
		if previous, found := byType[conditionType]; found && previous.Status == metav1.ConditionUnknown && !previous.LastTransitionTime.IsZero() {
			condition.LastTransitionTime = previous.LastTransitionTime
		}
		result = append(result, condition)
	}
	return result
}
