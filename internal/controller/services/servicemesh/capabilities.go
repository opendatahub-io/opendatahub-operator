package servicemesh

import (
	"errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	cond "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
)

func serviceMeshCondition(reason, message string) *common.Condition {
	return &common.Condition{
		Type:    status.CapabilityServiceMesh,
		Status:  metav1.ConditionTrue,
		Reason:  reason,
		Message: message,
	}
}

func authorizationCondition(reason, message string) *common.Condition {
	return &common.Condition{
		Type:    status.CapabilityServiceMeshAuthorization,
		Status:  metav1.ConditionTrue,
		Reason:  reason,
		Message: message,
	}
}

func createCapabilityReporter(cli client.Client, object *dsciv1.DSCInitialization, successfulCondition *common.Condition) *status.Reporter[*dsciv1.DSCInitialization] {
	return status.NewStatusReporter(
		cli,
		object,
		func(err error) status.SaveStatusFunc[*dsciv1.DSCInitialization] {
			return func(saved *dsciv1.DSCInitialization) {
				actualCondition := successfulCondition.DeepCopy()
				if err != nil {
					actualCondition.Status = metav1.ConditionFalse
					actualCondition.Message = err.Error()
					actualCondition.Reason = status.CapabilityFailed
					var missingOperatorErr *feature.MissingOperatorError
					if errors.As(err, &missingOperatorErr) {
						actualCondition.Reason = status.MissingOperatorReason
					}
				}
				cond.SetStatusCondition(&saved.Status, *actualCondition)
			}
		},
	)
}
