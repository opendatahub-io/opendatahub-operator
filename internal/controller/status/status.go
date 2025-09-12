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
	// PhaseNotReady is used when waiting for system to be ready after reconcile is successful
	// is an example of a constant that is not used anywhere in the code.
	PhaseNotReady = "Not Ready"

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
	ReconcileFailed           = "ReconcileFailed"
	ReconcileInit             = "ReconcileInit"
	ReconcileCompleted        = "ReconcileCompleted"
	ReconcileCompletedMessage = "Reconcile completed successfully"
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
	ConditionTypeProvisioningSucceeded           = "ProvisioningSucceeded"
	ConditionDeploymentsNotAvailableReason       = "DeploymentsNotReady"
	ConditionDeploymentsAvailable                = "DeploymentsAvailable"
	ConditionArgoWorkflowAvailable               = "ArgoWorkflowAvailable"
	ConditionTypeComponentsReady                 = "ComponentsReady"
	ConditionMonitoringAvailable                 = "MonitoringAvailable"
	ConditionMonitoringStackAvailable            = "MonitoringStackAvailable"
	ConditionTempoAvailable                      = "TempoAvailable"
	ConditionOpenTelemetryCollectorAvailable     = "OpenTelemetryCollectorAvailable"
	ConditionInstrumentationAvailable            = "InstrumentationAvailable"
	ConditionAlertingAvailable                   = "AlertingAvailable"
	ConditionThanosQuerierAvailable              = "ThanosQuerierAvailable"
	ConditionPersesAvailable                     = "PersesAvailable"
	ConditionPersesTempoDataSourceAvailable      = "PersesTempoDataSourceAvailable"
	ConditionPersesPrometheusDataSourceAvailable = "PersesPrometheusDataSourceAvailable"
	ConditionNodeMetricsEndpointAvailable        = "NodeMetricsEndpointAvailable"
)

const (
	MissingOperatorReason     string = "MissingOperator"
	ConfiguredReason          string = "Configured"
	RemovedReason             string = "Removed"
	UnmanagedReason           string = "Unmanaged"
	CapabilityFailed          string = "CapabilityFailed"
	ArgoWorkflowExist         string = "ArgoWorkflowExist"
	NoManagedComponentsReason        = "NoManagedComponents"

	AvailableReason = "Available"
	NotReadyReason  = "NotReady"
	ReadyReason     = "Ready"
)

const (
	ReadySuffix = "Ready"
)

const (
	DataSciencePipelinesDoesntOwnArgoCRDReason        = "DataSciencePipelinesDoesntOwnArgoCRD"
	DataSciencePipelinesArgoWorkflowsNotManagedReason = "DataSciencePipelinesArgoWorkflowsNotManaged"
	DataSciencePipelinesArgoWorkflowsCRDMissingReason = "DataSciencePipelinesArgoWorkflowsCRDMissing"

	DataSciencePipelinesDoesntOwnArgoCRDMessage = "Failed upgrade: workflows.argoproj.io CRD already exists but not deployed by this operator " +
		"remove existing Argo workflows or set `spec.components.aipipelines.managementState` to Removed to proceed"
	DataSciencePipelinesArgoWorkflowsNotManagedMessage = "Argo Workflows controllers are not managed by this operator"
	DataSciencePipelinesArgoWorkflowsCRDMissingMessage = "Argo Workflows controllers are not managed by this operator, but the CRD is missing"
)

// For Kueue MultiKueue CRD.
const (
	MultiKueueCRDReason  = "MultiKueueCRDV1Alpha1Exist"
	MultiKueueCRDMessage = "Kueue CRDs MultiKueueConfig v1alpha1 and/or MultiKueueCluster v1alpha1 exist, please remove them to proceed"

	KueueStateManagedNotSupported        = "KueueStateManagedNotSupported"
	KueueStateManagedNotSupportedMessage = "Kueue managementState Managed is not supported, please use Removed or Unmanaged"
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

	GatewayNotFoundMessage = "Gateway resource not found"
	GatewayNotReadyMessage = "Gateway is not ready"
	GatewayReadyMessage    = "Gateway is ready"

	// Gateway Authentication messages.
	AuthProxyDeployedMessage                 = "Auth proxy deployed successfully"
	AuthProxyFailedDeployMessage             = "Failed to deploy auth proxy"
	AuthProxyFailedOAuthClientMessage        = "Failed to create OAuth client"
	AuthProxyFailedCallbackRouteMessage      = "Failed to create auth callback route"
	AuthProxyFailedGenerateSecretMessage     = "Failed to generate client secret"
	AuthProxyOIDCModeWithoutConfigMessage    = "Cluster is in OIDC mode but GatewayConfig has no OIDC configuration"
	AuthProxyOIDCClientIDEmptyMessage        = "OIDC clientID cannot be empty"
	AuthProxyOIDCIssuerURLEmptyMessage       = "OIDC issuerURL cannot be empty"
	AuthProxyOIDCSecretRefNameEmptyMessage   = "OIDC clientSecretRef.name cannot be empty" //nolint:gosec // This is an error message, not a credential
	AuthProxyExternalAuthNoDeploymentMessage = "Cluster uses external authentication, no gateway auth proxy deployed"
)

// For v3 upgrade sanity checks.
const (
	CodeFlarePresentMessage = `Failed upgrade: CodeFlare component is present in the cluster. It must be uninstalled to proceed with Ray component upgrade.
To uninstall it, you should delete all RayClusters resources from the cluster, delete the CodeFlare component resource and recreate the RayClusters.`
)

// For JobSet operator checks.
const (
	JobSetOperatorNotInstalledMessage = "JobSet operator not installed, please install it first"
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
