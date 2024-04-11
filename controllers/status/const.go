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

// Condition.Message.
const (
	DSCIReconcileStartMessage   = "Initializing DSCInitialization resource"
	DSCIMissingMessage          = "Failed to get a valid DSCInitialization CR, please create DSCI instance"
	DSCIReconcileFailedMessage  = "Failed to reconcile DSCInitialization resource"
	DSCIReconcileSuccessMessage = "DSCInitialization reconciled successfully"

	DSCReconcileStartMessage   = "Initializing DataScienceCluster resource"
	DSCReconcileSuccessMessage = "DataScienceCluster reconciled successfully"
	TerminatingMessage         = "Resource is being terminated"
)

// Condition.Reason.
const (
	// DSPv2.
	ArgoWorkflowExistReason string = "ArgoWorkflowExist"
)
const (
	// DSCI ServiceMesh + Authorino.
	MissingOperatorReason string = "MissingOperator"
	ConfiguredReason      string = "Configured"
	RemovedReason         string = "Removed"
	CapabilityFailed      string = "CapabilityFailed"
)
const (
	// DSCI and DSC.
	DSCIMissingReason                           = "MissingDSCI"
	ReconcileStartReason                        = "ReconcileStart"
	ReconcileSuccessReason                      = "ReconcileCompleted"
	ReconcileFailedReason                       = "ReconcileFailed"
	ReconcileCompletedWithComponentErrorsReason = "ReconcileCompletedWithComponentErrors"
	// Remove.
	TerminatingReason  = "Terminating"
	RemoveFailedReason = "RemoveFailed"
)

// Condition.Type.
const (
	ConditionReconcileSuccess conditionsv1.ConditionType = "ReconcileSuccess"
)
const (
	// DSCI ServiceMesh + Authorino.
	CapabilityServiceMesh              conditionsv1.ConditionType = "CapabilityServiceMesh"
	CapabilityServiceMeshAuthorization conditionsv1.ConditionType = "CapabilityServiceMeshAuthorization"
)
const (
	// component DSPv2.
	CapabilityDSPv2Argo conditionsv1.ConditionType = "CapabilityDSPv2Argo"
)

// Phase.
const (
	PhaseError    = "Error"
	PhaseReady    = "Ready"
	PhaseCreated  = "Created"
	PhaseDeleting = "Deleting"
)
