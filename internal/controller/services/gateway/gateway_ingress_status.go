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
	additionalIngressReasonConfigurationInvalid  = "ConfigurationInvalid"
	additionalIngressReasonOwnershipConflict     = "OwnershipConflict"
	additionalIngressReasonStatusReadFailed      = "StatusReadFailed"
	additionalIngressReasonStatusUnavailable     = "StatusUnavailable"
	additionalIngressReasonNotReady              = "NotReady"
	additionalIngressReasonReady                 = "Ready"
	additionalIngressReasonMultipleControllers   = "MultipleIngressControllers"
	additionalIngressRouteServicesKey            = "gateway/additional-ingress-route-services"
)

var additionalIngressConditionTypes = []string{
	serviceApi.AdditionalIngressListenerReadyConditionType,
	serviceApi.AdditionalIngressRouteAdmittedConditionType,
	serviceApi.AdditionalIngressAuthenticationReadyConditionType,
	serviceApi.AdditionalIngressReadyConditionType,
}

// syncAdditionalIngressStatus inventories configured ingresses before the
// reconciliation actions report their per-ingress conditions.
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
	return nil
}

func additionalIngressRouteServices(rr *odhtypes.ReconciliationRequest) map[string]string {
	if rr.Extensions == nil {
		rr.Extensions = make(map[string]any)
	}
	if services, ok := rr.Extensions[additionalIngressRouteServicesKey].(map[string]string); ok && services != nil {
		return services
	}
	services := make(map[string]string)
	rr.Extensions[additionalIngressRouteServicesKey] = services
	return services
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
	if len(gatewayConfig.Spec.AdditionalIngresses) == 0 {
		return nil
	}

	l := logf.FromContext(ctx).WithName("syncAdditionalIngressReadiness")
	services := additionalIngressRouteServices(rr)
	gateway := &gwapiv1.Gateway{}
	gatewayErr := rr.Client.Get(ctx, types.NamespacedName{
		Name: GetDefaultGatewayName(), Namespace: GetGatewayNamespace(),
	}, gateway)
	retry := gatewayErr != nil && !k8serr.IsNotFound(gatewayErr)
	if retry {
		l.Error(gatewayErr, "Failed to read Gateway status for additional ingress listeners")
	}

	for _, ingress := range gatewayConfig.Spec.AdditionalIngresses {
		status := additionalIngressStatusByName(gatewayConfig, ingress.Name)
		if status == nil {
			continue
		}

		switch {
		case k8serr.IsNotFound(gatewayErr):
			setAdditionalIngressCondition(status, gatewayConfig.Generation,
				serviceApi.AdditionalIngressListenerReadyConditionType, metav1.ConditionFalse,
				additionalIngressReasonDependencyUnavailable, "The shared Gateway does not exist")
		case gatewayErr != nil:
			setAdditionalIngressCondition(status, gatewayConfig.Generation,
				serviceApi.AdditionalIngressListenerReadyConditionType, metav1.ConditionUnknown,
				additionalIngressReasonStatusReadFailed, "The shared Gateway status could not be read")
		default:
			conditions.SetStatusCondition(status, additionalIngressListenerCondition(
				gateway, ingress.Name, gatewayConfig.Generation,
			))
		}

		if err := syncAdditionalIngressRouteReadiness(ctx, rr, gatewayConfig, ingress, services[ingress.Name], status); err != nil {
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

func additionalIngressListenerCondition(
	gateway *gwapiv1.Gateway,
	listenerName string,
	generation int64,
) common.Condition {
	condition := func(status metav1.ConditionStatus, reason, message string) common.Condition {
		return common.Condition{
			Type:               serviceApi.AdditionalIngressListenerReadyConditionType,
			Status:             status,
			ObservedGeneration: generation,
			Reason:             reason,
			Message:            message,
		}
	}

	configured := false
	for _, listener := range gateway.Spec.Listeners {
		if string(listener.Name) == listenerName {
			configured = true
			break
		}
	}
	if !configured {
		return condition(metav1.ConditionFalse, additionalIngressReasonNotReady,
			fmt.Sprintf("Gateway listener %q is not configured", listenerName))
	}

	var listenerStatus *gwapiv1.ListenerStatus
	for i := range gateway.Status.Listeners {
		if string(gateway.Status.Listeners[i].Name) == listenerName {
			listenerStatus = &gateway.Status.Listeners[i]
			break
		}
	}
	if listenerStatus == nil {
		return condition(metav1.ConditionUnknown, serviceApi.AdditionalIngressReconciliationPendingReason,
			fmt.Sprintf("Gateway has not reported status for listener %q", listenerName))
	}

	requiredConditions := []gwapiv1.ListenerConditionType{
		gwapiv1.ListenerConditionAccepted,
		gwapiv1.ListenerConditionResolvedRefs,
		gwapiv1.ListenerConditionProgrammed,
	}
	var failedCondition *metav1.Condition
	var pendingReason, pendingMessage string
	pending := false
	for _, conditionType := range requiredConditions {
		observed := meta.FindStatusCondition(listenerStatus.Conditions, string(conditionType))
		if observed == nil {
			if !pending {
				pending = true
				pendingReason = serviceApi.AdditionalIngressReconciliationPendingReason
				pendingMessage = fmt.Sprintf("Gateway has not reported %s for listener %q", conditionType, listenerName)
			}
			continue
		}
		if observed.ObservedGeneration != gateway.Generation {
			if !pending {
				pending = true
				pendingReason = serviceApi.AdditionalIngressReconciliationPendingReason
				pendingMessage = fmt.Sprintf("Gateway listener %q status has not observed the current Gateway generation", listenerName)
			}
			continue
		}
		if observed.Status == metav1.ConditionFalse && failedCondition == nil {
			failedCondition = observed
		}
		if observed.Status != metav1.ConditionTrue && !pending {
			pending = true
			pendingReason = serviceApi.AdditionalIngressReconciliationPendingReason
			pendingMessage = fmt.Sprintf("Gateway listener %q has not reported %s as ready", listenerName, conditionType)
			if details := conditionDetails(observed.Reason, observed.Message); details != "" {
				pendingMessage += ": " + details
			}
		}
	}
	if failedCondition != nil {
		message := fmt.Sprintf("Gateway listener %q condition %s is False", listenerName, failedCondition.Type)
		if details := conditionDetails(failedCondition.Reason, failedCondition.Message); details != "" {
			message += ": " + details
		}
		return condition(metav1.ConditionFalse, additionalIngressReasonNotReady, message)
	}
	if pending {
		return condition(metav1.ConditionUnknown,
			firstNonEmpty(pendingReason, serviceApi.AdditionalIngressReconciliationPendingReason),
			firstNonEmpty(pendingMessage, fmt.Sprintf("Gateway listener %q readiness is pending", listenerName)))
	}
	return condition(metav1.ConditionTrue, additionalIngressReasonReady,
		fmt.Sprintf("Gateway listener %q is accepted, programmed, and has resolved references", listenerName))
}

func syncAdditionalIngressRouteReadiness(
	ctx context.Context,
	rr *odhtypes.ReconciliationRequest,
	gatewayConfig *serviceApi.GatewayConfig,
	ingress serviceApi.AdditionalIngress,
	serviceName string,
	ingressStatus *serviceApi.AdditionalIngressStatus,
) error {
	if serviceName == "" {
		return nil
	}

	route := &routev1.Route{}
	routeKey := client.ObjectKey{
		Name: GetAdditionalIngressRouteName(ingress.Name), Namespace: GetGatewayNamespace(),
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
		route.Spec.Port == nil || route.Spec.Port.TargetPort.IntVal != ingress.ListenerPort {
		setAdditionalIngressCondition(ingressStatus, gatewayConfig.Generation,
			serviceApi.AdditionalIngressRouteAdmittedConditionType, metav1.ConditionFalse,
			additionalIngressReasonNotReady,
			fmt.Sprintf("Route %q does not match the configured hostname, Service, and listener port", route.Name))
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
		status = metav1.ConditionUnknown
		reason = serviceApi.AdditionalIngressReconciliationPendingReason
		message = fmt.Sprintf("Waiting for IngressController %q to report Route admission", ingress.IngressControllerName)
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
		serviceApi.AdditionalIngressListenerReadyConditionType,
		serviceApi.AdditionalIngressRouteAdmittedConditionType,
		serviceApi.AdditionalIngressAuthenticationReadyConditionType,
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
			"Listener, Route, and authentication are ready")
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
		}
		status.Conditions = currentAdditionalIngressConditions(previousByName[spec.Name].Conditions, generation)
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
