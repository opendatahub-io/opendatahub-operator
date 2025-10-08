package dscinitialization

import (
	"errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
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

func createCapabilityReporter(cli client.Client, object *dsciv2.DSCInitialization, successfulCondition *common.Condition) *status.Reporter[*dsciv2.DSCInitialization] {
	return status.NewStatusReporter[*dsciv2.DSCInitialization](
		cli,
		object,
		func(err error) status.SaveStatusFunc[*dsciv2.DSCInitialization] {
			return func(saved *dsciv2.DSCInitialization) {
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
