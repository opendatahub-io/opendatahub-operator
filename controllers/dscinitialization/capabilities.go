package dscinitialization

import (
	"errors"

	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	corev1 "k8s.io/api/core/v1"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
)

func serviceMeshCondition(reason, message string) *conditionsv1.Condition {
	return &conditionsv1.Condition{
		Type:    status.CapabilityServiceMesh,
		Status:  corev1.ConditionTrue,
		Reason:  reason,
		Message: message,
	}
}

func authorizationCondition(reason, message string) *conditionsv1.Condition {
	return &conditionsv1.Condition{
		Type:    status.CapabilityServiceMeshAuthorization,
		Status:  corev1.ConditionTrue,
		Reason:  reason,
		Message: message,
	}
}

func createCapabilityReporter(condition *conditionsv1.Condition) *status.Reporter[*dsciv1.DSCInitialization] {
	return status.NewStatusReporter[*dsciv1.DSCInitialization](
		func(err error) status.SaveStatusFunc[*dsciv1.DSCInitialization] {
			return func(saved *dsciv1.DSCInitialization) {
				if err != nil {
					condition.Status = corev1.ConditionFalse
					condition.Message = err.Error()
					condition.Reason = status.CapabilityFailed
					var missingOperatorErr *feature.MissingOperatorError
					if errors.As(err, &missingOperatorErr) {
						condition.Reason = status.MissingOperatorReason
					}
				}
				conditionsv1.SetStatusCondition(&saved.Status.Conditions, *condition)
			}
		},
	)
}
