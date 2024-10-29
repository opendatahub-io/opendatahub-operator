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
	"os"
	"path/filepath"

	"github.com/blang/semver/v4"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	"github.com/operator-framework/api/pkg/lib/version"
	"gopkg.in/yaml.v2"
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
	CapabilityServiceMesh              conditionsv1.ConditionType = "CapabilityServiceMesh"
	CapabilityServiceMeshAuthorization conditionsv1.ConditionType = "CapabilityServiceMeshAuthorization"
	CapabilityDSPv2Argo                conditionsv1.ConditionType = "CapabilityDSPv2Argo"
)

const (
	MissingOperatorReason string = "MissingOperator"
	ConfiguredReason      string = "Configured"
	RemovedReason         string = "Removed"
	CapabilityFailed      string = "CapabilityFailed"
	ArgoWorkflowExist     string = "ArgoWorkflowExist"
)

const (
	ReadySuffix = "Ready"
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

// SetComponentCondition appends Condition Type with const ReadySuffix for given component
// when component finished reconcile.
func SetComponentCondition(conditions *[]conditionsv1.Condition, component string, reason string, message string, status corev1.ConditionStatus) {
	SetCondition(conditions, component+ReadySuffix, reason, message, status)
}

// RemoveComponentCondition remove Condition of giving component.
func RemoveComponentCondition(conditions *[]conditionsv1.Condition, component string) {
	conditionsv1.RemoveStatusCondition(conditions, conditionsv1.ConditionType(component+ReadySuffix))
}

// +k8s:deepcopy-gen=true
type ComponentReleaseStatus struct {
	DisplayName string                  `json:"displayname,omitempty"`
	Version     version.OperatorVersion `json:"version,omitempty"`
	RepoURL     string                  `json:"repourl,omitempty"`
}

// +k8s:deepcopy-gen=true
type ComponentStatus struct {
	Releases []ComponentReleaseStatus `json:"releases,omitempty"`
}

// +k8s:deepcopy-gen=true
type CodeFlareStatus struct {
	ComponentStatus `json:",inline"`
}

// +k8s:deepcopy-gen=true
type DashboardStatus struct {
	ComponentStatus `json:",inline"`
}

// +k8s:deepcopy-gen=true
type WorkbenchesStatus struct {
	ComponentStatus `json:",inline"`
}

// +k8s:deepcopy-gen=true
type ModelMeshServingStatus struct {
	ComponentStatus `json:",inline"`
}

// +k8s:deepcopy-gen=true
type DataSciencePipelinesStatus struct {
	ComponentStatus `json:",inline"`
}

// +k8s:deepcopy-gen=true
type KserveStatus struct {
	ComponentStatus `json:",inline"`
}

// +k8s:deepcopy-gen=true
type KueueStatus struct {
	ComponentStatus `json:",inline"`
}

// +k8s:deepcopy-gen=true
type RayStatus struct {
	ComponentStatus `json:",inline"`
}

// +k8s:deepcopy-gen=true
type TrustyAIStatus struct {
	ComponentStatus `json:",inline"`
}

// +k8s:deepcopy-gen=true
type ModelRegistryStatus struct {
	RegistriesNamespace string `json:"registriesNamespace,omitempty"`
	ComponentStatus     `json:",inline"`
}

// +k8s:deepcopy-gen=true
type TrainingOperatorStatus struct {
	ComponentStatus `json:",inline"`
}

// +k8s:deepcopy-gen=true
type ComponentsStatus struct {
	CodeFlare            *CodeFlareStatus            `json:"codeflare,omitempty"`
	Dashboard            *DashboardStatus            `json:"dashboard,omitempty"`
	DataSciencePipelines *DataSciencePipelinesStatus `json:"datasciencepipelines,omitempty"`
	ModelMeshServing     *ModelMeshServingStatus     `json:"modelmeshserving,omitempty"`
	ModelRegistry        *ModelRegistryStatus        `json:"modelregistry,omitempty"`
	Kserve               *KserveStatus               `json:"kserve,omitempty"`
	Kueue                *KueueStatus                `json:"kueue,omitempty"`
	Ray                  *RayStatus                  `json:"ray,omitempty"`
	TrustyAI             *TrustyAIStatus             `json:"trustyai,omitempty"`
	TrainingOperator     *TrainingOperatorStatus     `json:"trainingoperator,omitempty"`
	Workbenches          *WorkbenchesStatus          `json:"workbenches,omitempty"`
}

// +k8s:deepcopy-gen=true
type ReleaseFileMeta struct {
	Releases []ComponentReleaseStatusMeta `json:"releases,omitempty"`
}

// +k8s:deepcopy-gen=true
type ComponentReleaseStatusMeta struct {
	DisplayName string `yaml:"displayname,omitempty"`
	Version     string `yaml:"version,omitempty"`
	RepoURL     string `yaml:"repourl,omitempty"`
}

// GetReleaseVersion read .env file and parse env variables delimiter by "=".
// If version is not set or set to "", return empty {}.
func GetReleaseVersion(defaultManifestPath string, componentName string) ComponentStatus {
	var componentVersion semver.Version
	var releaseInfo ReleaseFileMeta
	var releaseStatus ComponentReleaseStatus
	componentReleaseStatus := make([]ComponentReleaseStatus, 0)

	yamlData, err := os.ReadFile(filepath.Join(defaultManifestPath, componentName, "releases.yaml"))
	if err != nil {
		return ComponentStatus{}
	}

	err = yaml.Unmarshal(yamlData, &releaseInfo)
	if err != nil {
		return ComponentStatus{}
	}

	for _, release := range releaseInfo.Releases {
		componentVersion, err = semver.Parse(release.Version)

		if err != nil {
			return ComponentStatus{}
		}

		releaseStatus = ComponentReleaseStatus{
			DisplayName: release.DisplayName,
			Version:     version.OperatorVersion{Version: componentVersion},
			RepoURL:     release.RepoURL,
		}
		componentReleaseStatus = append(componentReleaseStatus, releaseStatus)
	}

	return ComponentStatus{
		Releases: componentReleaseStatus,
	}
}
