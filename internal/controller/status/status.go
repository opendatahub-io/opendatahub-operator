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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	cond "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
)

// conditionsWrapper implements common.ConditionsAccessor for a slice of conditions.
type conditionsWrapper struct {
	conditions *[]common.Condition
}

func (w *conditionsWrapper) GetConditions() []common.Condition {
	return *w.conditions
}

func (w *conditionsWrapper) SetConditions(conditions []common.Condition) {
	*w.conditions = conditions
}

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
)

const (
	// ConditionTypeAvailable indicates whether the resource is available.
	ConditionTypeAvailable = "Available"
	// ConditionTypeProgressing indicates whether the resource is progressing.
	ConditionTypeProgressing = "Progressing"
	// ConditionTypeDegraded indicates whether the resource is degraded.
	ConditionTypeDegraded = "Degraded"
	// ConditionTypeUpgradeable indicates whether the resource is upgradeable.
	ConditionTypeUpgradeable = "Upgradeable"
	// ConditionTypeReady indicates whether the resource is ready.
	ConditionTypeReady = "Ready"
	// ConditionTypeReconcileComplete indicates whether reconciliation is complete.
	ConditionTypeReconcileComplete = "ReconcileComplete"

	// Component-specific condition types.
	ConditionTypeProvisioningSucceeded       = "ProvisioningSucceeded"
	ConditionDeploymentsNotAvailableReason   = "DeploymentsNotReady"
	ConditionDeploymentsAvailable            = "DeploymentsAvailable"
	ConditionServerlessAvailable             = "ServerlessAvailable"
	ConditionServiceMeshAvailable            = "ServiceMeshAvailable"
	ConditionArgoWorkflowAvailable           = "ArgoWorkflowAvailable"
	ConditionTypeComponentsReady             = "ComponentsReady"
	ConditionServingAvailable                = "ServingAvailable"
	ConditionMonitoringAvailable             = "MonitoringAvailable"
	ConditionMonitoringStackAvailable        = "MonitoringStackAvailable"
	ConditionTempoAvailable                  = "TempoAvailable"
	ConditionOpenTelemetryCollectorAvailable = "OpenTelemetryCollectorAvailable"
	ConditionInstrumentationAvailable        = "InstrumentationAvailable"
	ConditionAlertingAvailable               = "AlertingAvailable"
)

const (
	CapabilityServiceMesh              string = "CapabilityServiceMesh"
	CapabilityServiceMeshAuthorization string = "CapabilityServiceMeshAuthorization"
	CapabilityDSPv2Argo                string = "CapabilityDSPv2Argo"
)

const (
	MissingOperatorReason     string = "MissingOperator"
	ConfiguredReason          string = "Configured"
	RemovedReason             string = "Removed"
	UnmanagedReason           string = "Unmanaged"
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
	ServiceMeshNotConfiguredReason   = "ServiceMeshNotConfigured"
	ServiceMeshNotReadyReason        = "ServiceMeshNotReady"
	ServiceMeshNeedConfiguredMessage = "ServiceMesh needs to be set to 'Managed' in DSCI CR"
	ServiceMeshNotConfiguredMessage  = "ServiceMesh is not configured in DSCI CR"
	ServiceMeshNotReadyMessage       = "ServiceMesh is not ready"

	ServiceMeshOperatorNotInstalledReason  = "ServiceMeshOperatorNotInstalled"
	ServiceMeshOperatorNotInstalledMessage = "ServiceMesh operator must be installed for this component's configuration"

	ServerlessOperatorNotInstalledReason  = "ServerlessOperatorNotInstalled"
	ServerlessOperatorNotInstalledMessage = "Serverless operator must be installed for this component's configuration"

	ServerlessUnsupportedCertMessage = "Serverless certificate type is not supported"
)

const (
	DataSciencePipelinesDoesntOwnArgoCRDReason        = "DataSciencePipelinesDoesntOwnArgoCRD"
	DataSciencePipelinesArgoWorkflowsNotManagedReason = "DataSciencePipelinesArgoWorkflowsNotManaged"
	DataSciencePipelinesArgoWorkflowsCRDMissingReason = "DataSciencePipelinesArgoWorkflowsCRDMissing"

	DataSciencePipelinesDoesntOwnArgoCRDMessage = "Failed upgrade: workflows.argoproj.io CRD already exists but not deployed by this operator " +
		"remove existing Argo workflows or set `spec.components.datasciencepipelines.managementState` to Removed to proceed"
	DataSciencePipelinesArgoWorkflowsNotManagedMessage = "Argo Workflows controllers are not managed by this operator"
	DataSciencePipelinesArgoWorkflowsCRDMissingMessage = "Argo Workflows controllers are not managed by this operator, but the CRD is missing"
)

// For Kueue MultiKueue CRD.
const (
	MultiKueueCRDReason  = "MultiKueueCRDV1Alpha1Exist"
	MultiKueueCRDMessage = "Kueue CRDs MultiKueueConfig v1alpha1 and/or MultiKueueCluster v1alpha1 exist, please remove them to proceed"

	KueueOperatorAlreadyInstalleReason   = "KueueOperatorAlreadyInstalled"
	KueueOperatorAlreadyInstalledMessage = "Kueue operator already installed, uninstall it or change kueue component state to Unmanaged"
	KueueOperatorNotInstalleReason       = "KueueOperatorNotInstalleReason"
	KueueOperatorNotInstalledMessage     = "Kueue operator not installed, install it or change kueue component state to Managed"
)

// For TrustyAI require ISVC CRD.
const (
	ISVCMissingCRDReason  = "InferenceServiceCRDMissing"
	ISVCMissingCRDMessage = "InferenceServices CRD does not exist, please enable serving component first"
)

// For Monitoring service checks.
const (
	MetricsNotConfiguredReason  = "MetricsNotConfigured"
	MetricsNotConfiguredMessage = "Metrics not configured in DSCI CR"
	TracesNotConfiguredReason   = "TracesNotConfigured"
	TracesNotConfiguredMessage  = "Traces not configured in DSCI CR"

	AlertingNotConfiguredReason  = "AlertingNotConfigured"
	AlertingNotConfiguredMessage = "Alerting not configured in DSCI CR"

	TempoOperatorMissingMessage                  = "Tempo operator must be installed for traces configuration"
	COOMissingMessage                            = "ClusterObservability operator must be installed for metrics configuration"
	OpenTelemetryCollectorOperatorMissingMessage = "OpenTelemetryCollector operator must be installed for OpenTelemetry configuration"
)

// setConditions is a helper function to set multiple conditions at once.
func setConditions(wrapper *conditionsWrapper, conditions []common.Condition) {
	for _, c := range conditions {
		cond.SetStatusCondition(wrapper, c)
	}
}

// SetProgressingCondition sets the ProgressingCondition to True and other conditions to false or
// Unknown. Used when we are just starting to reconcile, and there are no existing conditions.
func SetProgressingCondition(conditions *[]common.Condition, reason string, message string) {
	wrapper := &conditionsWrapper{conditions: conditions}
	setConditions(wrapper, []common.Condition{
		{
			Type:    ConditionTypeReconcileComplete,
			Status:  metav1.ConditionUnknown,
			Reason:  reason,
			Message: message,
		},
		{
			Type:    ConditionTypeAvailable,
			Status:  metav1.ConditionFalse,
			Reason:  reason,
			Message: message,
		},
		{
			Type:    ConditionTypeProgressing,
			Status:  metav1.ConditionTrue,
			Reason:  reason,
			Message: message,
		},
		{
			Type:    ConditionTypeDegraded,
			Status:  metav1.ConditionFalse,
			Reason:  reason,
			Message: message,
		},
		{
			Type:    ConditionTypeUpgradeable,
			Status:  metav1.ConditionUnknown,
			Reason:  reason,
			Message: message,
		},
	})
}

// SetErrorCondition sets the ConditionTypeReconcileComplete to False in case of any errors
// during the reconciliation process.
func SetErrorCondition(conditions *[]common.Condition, reason string, message string) {
	wrapper := &conditionsWrapper{conditions: conditions}
	setConditions(wrapper, []common.Condition{
		{
			Type:    ConditionTypeReconcileComplete,
			Status:  metav1.ConditionFalse,
			Reason:  reason,
			Message: message,
		},
		{
			Type:    ConditionTypeAvailable,
			Status:  metav1.ConditionFalse,
			Reason:  reason,
			Message: message,
		},
		{
			Type:    ConditionTypeProgressing,
			Status:  metav1.ConditionFalse,
			Reason:  reason,
			Message: message,
		},
		{
			Type:    ConditionTypeDegraded,
			Status:  metav1.ConditionTrue,
			Reason:  reason,
			Message: message,
		},
		{
			Type:    ConditionTypeUpgradeable,
			Status:  metav1.ConditionFalse,
			Reason:  reason,
			Message: message,
		},
	})
}

// SetCompleteCondition sets the ConditionTypeReconcileComplete to True and other Conditions
// to indicate that the reconciliation process has completed successfully.
func SetCompleteCondition(conditions *[]common.Condition, reason string, message string) {
	wrapper := &conditionsWrapper{conditions: conditions}
	setConditions(wrapper, []common.Condition{
		{
			Type:    ConditionTypeReconcileComplete,
			Status:  metav1.ConditionTrue,
			Reason:  reason,
			Message: message,
		},
		{
			Type:    ConditionTypeAvailable,
			Status:  metav1.ConditionTrue,
			Reason:  reason,
			Message: message,
		},
		{
			Type:    ConditionTypeProgressing,
			Status:  metav1.ConditionFalse,
			Reason:  reason,
			Message: message,
		},
		{
			Type:    ConditionTypeDegraded,
			Status:  metav1.ConditionFalse,
			Reason:  reason,
			Message: message,
		},
		{
			Type:    ConditionTypeUpgradeable,
			Status:  metav1.ConditionTrue,
			Reason:  reason,
			Message: message,
		},
	})
	cond.RemoveStatusCondition(wrapper, CapabilityDSPv2Argo)
}

// SetCondition is a general purpose function to update any type of condition.
func SetCondition(conditions *[]common.Condition, conditionType string, reason string, message string, status metav1.ConditionStatus) {
	wrapper := &conditionsWrapper{conditions: conditions}
	cond.SetStatusCondition(wrapper, common.Condition{
		Type:    conditionType,
		Status:  status,
		Reason:  reason,
		Message: message,
	})
}
