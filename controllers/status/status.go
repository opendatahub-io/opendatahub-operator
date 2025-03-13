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

// Package status contains different conditions, phases and progresses,
// being used by DataScienceCluster and DSCInitialization's controller
package status

import (
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	corev1 "k8s.io/api/core/v1"
)

// These constants represent the overall Phase as used by .Status.Phase.
const (
	// PhaseIgnored is used when a resource is ignored
	// is an example of a constant that is not used anywhere in the code.
	PhaseIgnored = "Ignored"
	// PhaseNotReady is used when waiting for system to be ready after reconcile is successful
	// is an example of a constant that is not used anywhere in the code.
	PhaseNotReady = "Not Ready"
	// PhaseClusterExpanding is used when cluster is expanding capacity
	// is an example of a constant that is not used anywhere in the code.
	PhaseClusterExpanding = "Expanding Capacity"
	// PhaseDeleting is used when cluster is deleting
	// is an example of a constant that is not used anywhere in the code.
	PhaseDeleting = "Deleting"
	// PhaseConnecting is used when cluster is connecting to external cluster
	// is an example of a constant that is not used anywhere in the code.
	PhaseConnecting = "Connecting"
	// PhaseOnboarding is used when consumer is Onboarding
	// is an example of a constant that is not used anywhere in the code.
	PhaseOnboarding = "Onboarding"

	// PhaseProgressing is used when SetProgressingCondition() is called.
	PhaseProgressing = "Progressing"
	// PhaseError is used when SetErrorCondition() is called.
	PhaseError = "Error"
	// PhaseReady is used when SetCompleteCondition is called.
	PhaseReady = "Ready"
)

// List of constants to show different reconciliation messages and statuses.
const (
	// ReconcileFailed is used when multiple DSCI instance exists or DSC reconcile failed/removal failed.
	ReconcileFailed                       = "ReconcileFailed"
	ReconcileInit                         = "ReconcileInit"
	ReconcileCompleted                    = "ReconcileCompleted"
	ReconcileCompletedWithComponentErrors = "ReconcileCompletedWithComponentErrors"
	ReconcileCompletedMessage             = "Reconcile completed successfully"

	// ConditionReconcileComplete represents extra Condition Type, used by .Condition.Type.
	ConditionReconcileComplete conditionsv1.ConditionType = "ReconcileComplete"
)

const (
	ConditionTypeReady                     = "Ready"
	ConditionTypeProvisioningSucceeded     = "ProvisioningSucceeded"
	ConditionDeploymentsNotAvailableReason = "DeploymentsNotReady"
	ConditionDeploymentsAvailable          = "DeploymentsAvailable"
	ConditionServerlessAvailable           = "ServerlessAvailable"
	ConditionServiceMeshAvailable          = "ServiceMeshAvailable"
	ConditionArgoWorkflowAvailable         = "ArgoWorkflowAvailable"
	ConditionTypeComponentsReady           = "ComponentsReady"
	ConditionServingAvailable              = "ServingAvailable"
)

const (
	CapabilityServiceMesh              conditionsv1.ConditionType = "CapabilityServiceMesh"
	CapabilityServiceMeshAuthorization conditionsv1.ConditionType = "CapabilityServiceMeshAuthorization"
	CapabilityDSPv2Argo                conditionsv1.ConditionType = "CapabilityDSPv2Argo"
)

const (
	MissingOperatorReason     string = "MissingOperator"
	ConfiguredReason          string = "Configured"
	RemovedReason             string = "Removed"
	CapabilityFailed          string = "CapabilityFailed"
	ArgoWorkflowExist         string = "ArgoWorkflowExist"
	NoManagedComponentsReason        = "NoManagedComponents"

	DegradedReason  = "Degraded"
	AvailableReason = "Available"
	UnknownReason   = "Unknown"
	NotReadyReason  = "NotReady"
	ErrorReason     = "Error"
	ReadyReason     = "Ready"
)

const (
	ReadySuffix = "Ready"
)

const (
	ServiceMeshNotConfiguredReason  = "ServiceMeshNotConfigured"
	ServiceMeshNotConfiguredMessage = "ServiceMesh needs to be set to 'Managed' in DSCI CR"

	ServiceMeshOperatorNotInstalledReason  = "ServiceMeshOperatorNotInstalled"
	ServiceMeshOperatorNotInstalledMessage = "ServiceMesh operator must be installed for this component's configuration"

	ServerlessOperatorNotInstalledReason  = "ServerlessOperatorNotInstalled"
	ServerlessOperatorNotInstalledMessage = "Serverless operator must be installed for this component's configuration"
)

const (
	DataSciencePipelinesDoesntOwnArgoCRDReason = "DataSciencePipelinesDoesntOwnArgoCRD"

	DataSciencePipelinesDoesntOwnArgoCRDMessage = "Failed upgrade: workflows.argoproj.io CRD already exists but not deployed by this operator " +
		"remove existing Argo workflows or set `spec.components.datasciencepipelines.managementState` to Removed to proceed"
)

// For Kueue MultiKueue CRD.
const (
	MultiKueueCRDReason  = "MultiKueueCRDV1Alpha1Exist"
	MultiKueueCRDMessage = "MultiKueue CRDs MultiKueueConfig v1alpha1 and MultiKueueCluster v1Alpha1 exist, please remove them to proceed"
)

// For TrustyAI require ISVC CRD.
const (
	ISVCMissingCRDReason  = "InferenceServiceCRDMissing"
	ISVCMissingCRDMessage = "InferenceServices CRD does not exist, please enable serving component first"
)

// SetProgressingCondition sets the ProgressingCondition to True and other conditions to false or
// Unknown. Used when we are just starting to reconcile, and there are no existing conditions.
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
		Status:  corev1.ConditionFalse,
		Reason:  reason,
		Message: message,
	})
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
	conditionsv1.RemoveStatusCondition(conditions, CapabilityDSPv2Argo)
}

// SetCondition is a general purpose function to update any type of condition.
func SetCondition(conditions *[]conditionsv1.Condition, conditionType string, reason string, message string, status corev1.ConditionStatus) {
	conditionsv1.SetStatusCondition(conditions, conditionsv1.Condition{
		Type:    conditionsv1.ConditionType(conditionType),
		Status:  status,
		Reason:  reason,
		Message: message,
	})
}
