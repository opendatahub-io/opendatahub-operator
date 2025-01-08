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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/apis/common"
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

	ConditionTypeReady string = "Ready"
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

const (
	ServiceMeshNotConfiguredReason  = "ServiceMeshNotConfigured"
	ServiceMeshNotConfiguredMessage = "ServiceMesh needs to be set to 'Managed' in DSCI CR"

	ServiceMeshOperatorNotInstalledReason  = "ServiceMeshOperatorNotInstalled"
	ServiceMeshOperatorNotInstalledMessage = "ServiceMesh operator must be installed for this component's configuration"

	ServerlessOperatorNotInstalledReason  = "ServerlessOperatorNotInstalled"
	ServerlessOperatorNotInstalledMessage = "Serverless operator must be installed for this component's configuration"
)

const (
	DataSciencePipelinesDoesntOwnArgoCRDReason  = "DataSciencePipelinesDoesntOwnArgoCRD"
	DataSciencePipelinesDoesntOwnArgoCRDMessage = "Failed upgrade: workflows.argoproj.io CRD already exists but not deployed by this operator " +
		"remove existing Argo workflows or set `spec.components.datasciencepipelines.managementState` to Removed to proceed"
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

// ModelRegistryStatus struct holds the status for the ModelRegistry component.
type ModelRegistryStatus struct {
	RegistriesNamespace string `json:"registriesNamespace,omitempty"`
}

func SetStatusCondition(obj common.WithStatus, condition metav1.Condition) bool {
	s := obj.GetStatus()
	return meta.SetStatusCondition(&s.Conditions, condition)
}

// +k8s:deepcopy-gen=true
type ReleaseFileMeta struct {
	Releases []ComponentReleaseStatusMeta `json:"releases,omitempty"`
}

// +k8s:deepcopy-gen=true
type ComponentReleaseStatusMeta struct {
	Name    string `yaml:"name,omitempty"`
	Version string `yaml:"version,omitempty"`
	RepoURL string `yaml:"repoURL,omitempty"`
}

// +k8s:deepcopy-gen=true
type ComponentReleaseStatus struct {
	Name    string                  `json:"name,omitempty"`
	Version version.OperatorVersion `json:"version,omitempty"`
	RepoURL string                  `json:"repoURL,omitempty"`
}

// GetReleaseStatus reads odh_metadata.yaml file and parses release information.
// If version is not set or set to "", return empty slice along with error.
func GetReleaseStatus(defaultManifestPath string, componentName string) ([]ComponentReleaseStatus, error) {
	var componentVersion semver.Version
	var releaseInfo ReleaseFileMeta
	var releaseStatus ComponentReleaseStatus
	componentReleaseStatus := make([]ComponentReleaseStatus, 0)

	yamlData, err := os.ReadFile(filepath.Join(defaultManifestPath, componentName, "odh_metadata.yaml"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	err = yaml.Unmarshal(yamlData, &releaseInfo)
	if err != nil {
		return nil, err
	}

	for _, release := range releaseInfo.Releases {
		componentVersion, err = semver.Parse(release.Version)

		if err != nil {
			return nil, err
		}

		releaseStatus = ComponentReleaseStatus{
			Name:    release.Name,
			Version: version.OperatorVersion{Version: componentVersion},
			RepoURL: release.RepoURL,
		}
		componentReleaseStatus = append(componentReleaseStatus, releaseStatus)
	}

	return componentReleaseStatus, nil
}
