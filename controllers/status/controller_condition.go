package status

import (
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	corev1 "k8s.io/api/core/v1"
)

// DSCI / DSC start(just created).
// type: Progressing
// statue: True
// Reason: ReconcileStart.
func InitControllerCondition(message string) *conditionsv1.Condition {
	return &conditionsv1.Condition{
		Type:    conditionsv1.ConditionProgressing,
		Status:  corev1.ConditionTrue,
		Reason:  ReconcileStartReason,
		Message: message,
	}
}

// SetCompleteCondition sets all conditions to indicate reconciliation process has completed and successfully.
// type: 	Available|Progressing|Degraded|ReconcileSuccess
// status: 	True	 |False		 |False	  |True
func SetCompleteCondition(conditions *[]conditionsv1.Condition, reason string, message string) {
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type:    ConditionReconcileSuccess,
		Status:  corev1.ConditionTrue,
		Reason:  reason,
		Message: message,
	})
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type:    conditionsv1.ConditionAvailable,
		Status:  corev1.ConditionTrue,
		Reason:  reason,
		Message: message,
	})
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type:    conditionsv1.ConditionProgressing,
		Status:  corev1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type:    conditionsv1.ConditionDegraded,
		Status:  corev1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
}

// SetErrorCondition sets all conditions to indicate reconciliation process will continue and it is in error.
// But the CR(DSCI and DSC) are available as it is functional
// type: 	Available|Progressing|Degraded|ReconcileSuccess
// status: 	True	 |True		 |True	  |False
func SetErrorCondition(conditions *[]conditionsv1.Condition, reason string, message string) {
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type:    ConditionReconcileSuccess,
		Status:  corev1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type:    conditionsv1.ConditionAvailable,
		Status:  corev1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type:    conditionsv1.ConditionProgressing,
		Status:  corev1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type:    conditionsv1.ConditionDegraded,
		Status:  corev1.ConditionTrue,
		Reason:  reason,
		Message: message,
	})
}

// UnavailableCondition sets available conditions to false to indicate resource is in terminating process.
// type: 	Available
// status: 	False
func UnavailableCondition(reason string, message string) *conditionsv1.Condition {
	return &conditionsv1.Condition{
		Type:    conditionsv1.ConditionAvailable,
		Status:  corev1.ConditionFalse,
		Reason:  reason,
		Message: message,
	}
}

// UpdateCondition is top caller in controllers to handle different case: init, error, success etc.
// newCondition is the condition to be updated from  the existing conditions list.
func UpdateCondition(conditions *[]conditionsv1.Condition, newCondition conditionsv1.Condition) {
	conditionsv1.SetStatusCondition(conditions, newCondition)
}

// RemoveComponentCondition remove Condition Type from given component when component changed to "not managed".
func RemoveComponentCondition(conditions *[]conditionsv1.Condition, condType conditionsv1.ConditionType) {
	conditionsv1.RemoveStatusCondition(conditions, condType)
}
