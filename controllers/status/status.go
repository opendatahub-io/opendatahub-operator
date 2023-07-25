/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package status

import (
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	corev1 "k8s.io/api/core/v1"
)

// These constants represent the overall Phase as used by .Status.Phase
const (
	// PhaseIgnored is used when a resource is ignored
	PhaseIgnored = "Ignored"
	// PhaseProgressing is used when SetProgressingCondition is called
	PhaseProgressing = "Progressing"
	// PhaseError is used when SetErrorCondition is called
	PhaseError = "Error"
	// PhaseReady is used when SetCompleteCondition is called
	PhaseReady = "Ready"
	// PhaseNotReady is used when waiting for system to be ready
	// after reconcile is successful
	PhaseNotReady = "Not Ready"
	// PhaseClusterExpanding is used when cluster is expanding capacity
	PhaseClusterExpanding = "Expanding Capacity"
	// PhaseDeleting is used when cluster is deleting
	PhaseDeleting = "Deleting"
	// PhaseConnecting is used when cluster is connecting to external cluster
	PhaseConnecting = "Connecting"
	// PhaseOnboarding is used when consumer is Onboarding
	PhaseOnboarding = "Onboarding"
)

// List of constants to show different different reconciliation messages and statuses.
const (
	ReconcileFailed           = "ReconcileFailed"
	ReconcileInit             = "ReconcileInit"
	ReconcileCompleted        = "ReconcileCompleted"
	ReconcileCompletedMessage = "Reconcile completed successfully"
)

const (
	ConditionReconcileComplete conditionsv1.ConditionType = "ReconcileComplete"
)

const (
	ReadySuffix = "Ready"
)

// SetProgressingCondition sets the ProgressingCondition to True and other conditions to
// false or Unknown. Used when we are just starting to reconcile, and there are no existing
// conditions.
func SetProgressingCondition(conditions *[]conditionsv1.Condition, reason string, message string) {
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type:    ConditionReconcileComplete,
		Status:  corev1.ConditionUnknown,
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
		Status:  corev1.ConditionTrue,
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
		Status:  corev1.ConditionUnknown,
		Reason:  reason,
		Message: message,
	})
}

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

func SetComponentCondition(conditions *[]conditionsv1.Condition, component string, reason string, message string, status corev1.ConditionStatus) {
	condtype := component + ReadySuffix
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type:    conditionsv1.ConditionType(condtype),
		Status:  status,
		Reason:  reason,
		Message: message,
	})
}

func RemoveComponentCondition(conditions *[]conditionsv1.Condition, component string) {
	condtype := component + ReadySuffix
	conditionsv1.RemoveStatusCondition(conditions, conditionsv1.ConditionType(condtype))
}

// SetCompleteCondition sets the ConditionReconcileComplete to True and other Conditions
// to indicate that the reconciliation process has completed successfully.
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

// won't override a status condition of the same type and status
func setStatusConditionIfNotPresent(conditions *[]conditionsv1.Condition, condition conditionsv1.Condition) {

	foundCondition := conditionsv1.FindStatusCondition(*conditions, condition.Type)
	if foundCondition != nil && foundCondition.Status == condition.Status {
		// already exists
		return
	}

	conditionsv1.SetStatusCondition(conditions, condition)
}
