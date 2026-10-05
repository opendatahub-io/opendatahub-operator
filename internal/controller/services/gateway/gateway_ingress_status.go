package gateway

import (
	"context"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

var additionalIngressConditionTypes = []string{
	serviceApi.AdditionalIngressListenerReadyConditionType,
	serviceApi.AdditionalIngressRouteAdmittedConditionType,
	serviceApi.AdditionalIngressAuthenticationReadyConditionType,
	serviceApi.AdditionalIngressReadyConditionType,
}

// syncAdditionalIngressStatus inventories configured ingresses before resource reconciliation.
// Readiness is initialized here; child-resource reconcilers publish later observations.
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
		if previousStatus, found := previousByName[spec.Name]; found {
			status.Conditions = currentAdditionalIngressConditions(previousStatus.Conditions, generation)
		}
		if len(status.Conditions) == 0 {
			status.Conditions = pendingAdditionalIngressConditions(generation)
		}
		statuses = append(statuses, status)
	}

	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].Name < statuses[j].Name
	})
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

func pendingAdditionalIngressConditions(generation int64) []common.Condition {
	now := metav1.Now()
	conditions := make([]common.Condition, 0, len(additionalIngressConditionTypes))
	for _, conditionType := range additionalIngressConditionTypes {
		conditions = append(conditions, common.Condition{
			Type:               conditionType,
			Status:             metav1.ConditionUnknown,
			ObservedGeneration: generation,
			LastTransitionTime: now,
			Reason:             serviceApi.AdditionalIngressReconciliationPendingReason,
			Message:            "Ingress readiness has not been observed yet",
		})
	}
	return conditions
}
