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
)

// The pattern of using phase is deprecated. Newer API types should use conditions instead.
// These constants represent the overall Phase as used by .Status.Phase.
// const (
// 	// PhaseIgnored is used when a resource is ignored
// 	// is an example of a constant that is not used anywhere in the code.
// 	PhaseIgnored = "Ignored"
// 	// PhaseNotReady is used when waiting for system to be ready after reconcile is successful
// 	// is an example of a constant that is not used anywhere in the code.
// 	PhaseNotReady = "Not Ready"
// 	// PhaseClusterExpanding is used when cluster is expanding capacity
// 	// is an example of a constant that is not used anywhere in the code.
// 	PhaseClusterExpanding = "Expanding Capacity"
// 	// PhaseDeleting is used when cluster is deleting
// 	// is an example of a constant that is not used anywhere in the code.
// 	PhaseDeleting = "Deleting"
// 	// PhaseConnecting is used when cluster is connecting to external cluster
// 	// is an example of a constant that is not used anywhere in the code.
// 	PhaseConnecting = "Connecting"
// 	// PhaseOnboarding is used when consumer is Onboarding
// 	// is an example of a constant that is not used anywhere in the code.
// 	PhaseOnboarding = "Onboarding"

// 	// PhaseProgressing is used when SetProgressingCondition() is called.
// 	PhaseProgressing = "Progressing"
// 	// PhaseError is used when SetErrorCondition() is called.
// 	PhaseError = "Error"
// 	// PhaseReady is used when SetCompleteCondition is called.
// 	PhaseReady = "Ready"
// )

// DSCI and DSC common Condition.Type.
const (
	// general.
	Created conditionsv1.ConditionType = "Created" // This can be used for any reason we cannot create CR, e.g CRD is missing
	//	Iniitialized conditionsv1.ConditionType = "Initialized"
	Progress conditionsv1.ConditionType = "Progressing" // map to old PhaseProgressing = "Progressing"
	Ready    conditionsv1.ConditionType = "Ready"       // map to old PhaseReady = "Ready"
	Error    conditionsv1.ConditionType = "Error"       // map to old PhaseError = "Error"
	Deleting conditionsv1.ConditionType = "Deleting"    // map to old PhaseDeleting = "Deleting"
)

// DSCI Condition.Type.
const (
	// servicemech + auth.
	CapabilityServiceMesh              conditionsv1.ConditionType = "CapabilityServiceMesh"
	CapabilityServiceMeshAuthorization conditionsv1.ConditionType = "CapabilityServiceMeshAuthorization"
)

// DSC Condition.Type.
const (
	// component DSPv2 Condition.Type.
	CapabilityDSPv2Argo conditionsv1.ConditionType = "CapabilityDSPv2Argo"
)

// DSCI and DSC common Condition.Reason.
const (
	ReconcileStart     = "ReconcileStart"
	ReconcileCompleted = "ReconcileCompleted"
	// ReconcileFailed is used when multiple DSCI instance exists or DSC reconcile failed/removal failed.
	ReconcileFailed                       = "ReconcileFailed"
	ReconcileCompletedWithComponentErrors = "ReconcileCompletedWithComponentErrors"

	// Removed.
	RemoveFailed = "RemoveFailed"

	// ConditionReconcileComplete represents extra Condition Type, used by .Condition.Type.
	ConditionReconcileComplete conditionsv1.ConditionType = "ReconcileComplete"
)

// DSCI ServiceMesh + Authorino Condition.Reason.
const (
	MissingOperatorReason string = "MissingOperator"
	ConfiguredReason      string = "Configured"
	RemovedReason         string = "Removed"
	CapabilityFailed      string = "CapabilityFailed"
)

// DSC component Condition.Reason.
const (
	// DSPv2  Condition.Reason.
	ArgoWorkflowReason string = "ArgoWorkflowExist"
)

// DSCI and DSC common Condition.Message.
const (
	DSCIReconcileStartMessage  = "Initializing DSCInitialization resource"
	DSCReconcileStartMessage   = "Initializing DataScienceCluster resource"
	ReconcileCompletedMessage  = "Reconcile completed successfully"
	DSCIMissingMessage         = "Failed to get a valid DSCInitialization CR, please create a DSCI instance"
	DSCIReconcileFailedMessage = "Failed to reconcile DSCInitialization resource"
)
