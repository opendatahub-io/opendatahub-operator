package status

import (
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	corev1 "k8s.io/api/core/v1"
)

// DSCI start.
func SetDefaultDSCIConditionInit() *conditionsv1.Condition {
	return &conditionsv1.Condition{
		Type:    Progress,
		Status:  corev1.ConditionTrue,
		Reason:  ReconcileStart,
		Message: DSCIReconcileStartMessage,
	}
}

// DSCI finish.
func SetDefaultDSCIConditionComplete() *conditionsv1.Condition {
	return &conditionsv1.Condition{
		Type:    Ready,
		Status:  corev1.ConditionTrue,
		Reason:  ReconcileCompleted,
		Message: ReconcileCompletedMessage,
	}
}

// DSC start.
func SetDefaultDSCCondition() *conditionsv1.Condition {
	return &conditionsv1.Condition{
		Type:    Created,
		Status:  corev1.ConditionTrue,
		Reason:  ReconcileStart,
		Message: DSCReconcileStartMessage,
	}
}

// Component init.
func SetInitComponentCondition(componentName string, enabled bool) conditionsv1.Condition {
	managementstatue := "not Managed"
	if enabled {
		managementstatue = "Managed"
	}
	return conditionsv1.Condition{
		Type:    conditionsv1.ConditionType(componentName + string(Ready)),
		Status:  corev1.ConditionUnknown,
		Reason:  ReconcileStart,
		Message: "Component is " + managementstatue,
	}
}

// Component reconilce success.
func GetDefaultComponentCondition(componentName string) conditionsv1.Condition {
	return conditionsv1.Condition{
		Type:    conditionsv1.ConditionType(componentName + string(Ready)),
		Status:  corev1.ConditionTrue,
		Reason:  ReconcileCompleted,
		Message: "Component reconciled successfully",
	}
}

// Component failed reconcile condition, called by UpdateFailedCondition.
func setFailedComponentCondition(componentName string) conditionsv1.Condition {
	return conditionsv1.Condition{
		Type:   conditionsv1.ConditionType(componentName + string(Ready)),
		Status: corev1.ConditionFalse,
		Reason: ReconcileFailed,
	}
}

// Component failed with detail message from comonents reconcile.
func UpdateFailedCondition(componentName string, err error) (conditionsv1.Condition, error) {
	FailedCondition := setFailedComponentCondition(componentName)
	FailedCondition.Message = err.Error()
	return FailedCondition, err
}

// SetComponentCondition appends Condition Type with const Ready for given component
// when component finished reconcile.
func SetComponentCondition(conditions *[]conditionsv1.Condition, newCondition conditionsv1.Condition) {
	conditionsv1.SetStatusCondition(conditions, newCondition)
}

// RemoveComponentCondition remove Condition of giving component.
func RemoveComponentCondition(conditions *[]conditionsv1.Condition, condType conditionsv1.ConditionType) {
	// condType := component + string(Ready)
	conditionsv1.RemoveStatusCondition(conditions, condType)
}

// SetProgressingCondition sets the ProgressingCondition to True and other conditions to false or
// Unknown. Used when we are just starting to reconcile, and there are no existing conditions.
// func SetProgressingCondition(conditions *[]conditionsv1.Condition, reason string, message string) {
// 	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
// 		Type:    ConditionReconcileComplete,
// 		Status:  corev1.ConditionUnknown,
// 		Reason:  reason,
// 		Message: message,
// 	})
// 	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
// 		Type:    conditionsv1.ConditionAvailable,
// 		Status:  corev1.ConditionFalse,
// 		Reason:  reason,
// 		Message: message,
// 	})
// 	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
// 		Type:    conditionsv1.ConditionProgressing,
// 		Status:  corev1.ConditionTrue,
// 		Reason:  reason,
// 		Message: message,
// 	})
// 	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
// 		Type:    conditionsv1.ConditionDegraded,
// 		Status:  corev1.ConditionFalse,
// 		Reason:  reason,
// 		Message: message,
// 	})
// 	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
// 		Type:    conditionsv1.ConditionUpgradeable,
// 		Status:  corev1.ConditionUnknown,
// 		Reason:  reason,
// 		Message: message,
// 	})
// }

// SetErrorCondition sets the ConditionReconcileComplete to False in case of any errors
// during the reconciliation process.
func SetErrorCondition(conditions *[]conditionsv1.Condition, reason string, message string) {
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type:    ConditionReconcileComplete,
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
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type:    conditionsv1.ConditionUpgradeable,
		Status:  corev1.ConditionUnknown,
		Reason:  reason,
		Message: message,
	})
}

// DSC reconcile start.
func SetReconcileDSCCondition(reason, message string) *conditionsv1.Condition {
	return &conditionsv1.Condition{
		Type:    Created,
		Status:  corev1.ConditionFalse,
		Reason:  reason,
		Message: message,
	}
}

// SetCompleteCondition sets the ConditionReconcileComplete to True and other Conditions
// to indicate that the reconciliation process has completed successfully.
// DSC reconcile success.
func SetCompleteCondition(conditions *[]conditionsv1.Condition, reason string, message string) {
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type:    ConditionReconcileComplete,
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
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type:    conditionsv1.ConditionUpgradeable,
		Status:  corev1.ConditionTrue,
		Reason:  reason,
		Message: message,
	})
}

// Extra on compoment: Argo.
func SetExistingArgoCondition(reason, message string) conditionsv1.Condition {
	return conditionsv1.Condition{
		Type:    CapabilityDSPv2Argo,
		Status:  corev1.ConditionFalse,
		Reason:  reason,
		Message: message,
	}
	// SetComponentCondition(conditions, datasciencepipelines.ComponentName, ReconcileFailed, message, corev1.ConditionFalse)
}
