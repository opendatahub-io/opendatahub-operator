package dscinitialization

import (
	"errors"

	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
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

func createCapabilityReporter(cli client.Client, object *dsciv1.DSCInitialization, successfulCondition *conditionsv1.Condition) *status.Reporter[*dsciv1.DSCInitialization] {
	return status.NewStatusReporter[*dsciv1.DSCInitialization](
		cli,
		object,
		func(err error) status.SaveStatusFunc[*dsciv1.DSCInitialization] {
			return func(saved *dsciv1.DSCInitialization) {
				actualCondition := successfulCondition.DeepCopy()
				if err != nil {
					actualCondition.Status = corev1.ConditionFalse
					actualCondition.Message = err.Error()
					actualCondition.Reason = status.CapabilityFailed
					var missingOperatorErr *feature.MissingOperatorError
					if errors.As(err, &missingOperatorErr) {
						actualCondition.Reason = status.MissingOperatorReason
					}
				}
				conditionsv1.SetStatusCondition(&saved.Status.Conditions, *actualCondition)
			}
		},
	)
}
