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

package v1alpha1

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	TrainingOperatorComponentName = "trainingoperator"
	// value should match whats set in the XValidation below
	TrainingOperatorInstanceName = "default-" + TrainingOperatorComponentName
	TrainingOperatorKind         = "TrainingOperator"
)

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = (*TrainingOperator)(nil)

// NOTE: json tags are required. Any new fields you add must have json tags for the fields to be serialized.

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-trainingoperator'",message="TrainingOperator name must be default-trainingoperator"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// TrainingOperator is the Schema for the trainingoperators API
type TrainingOperator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TrainingOperatorSpec   `json:"spec,omitempty"`
	Status TrainingOperatorStatus `json:"status,omitempty"`
}

// TrainingOperatorSpec defines the desired state of TrainingOperator
type TrainingOperatorSpec struct {
	TrainingOperatorCommonSpec `json:",inline"`
}

type TrainingOperatorCommonSpec struct {
	common.DevFlagsSpec `json:",inline"`
}

// TrainingOperatorCommonStatus defines the shared observed state of TrainingOperator
type TrainingOperatorCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// TrainingOperatorStatus defines the observed state of TrainingOperator
type TrainingOperatorStatus struct {
	common.Status                `json:",inline"`
	TrainingOperatorCommonStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// TrainingOperatorList contains a list of TrainingOperator
type TrainingOperatorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TrainingOperator `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TrainingOperator{}, &TrainingOperatorList{})
}

func (c *TrainingOperator) GetDevFlags() *common.DevFlags {
	return c.Spec.DevFlags
}

func (c *TrainingOperator) GetStatus() *common.Status {
	return &c.Status.Status
}

func (c *TrainingOperator) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *TrainingOperator) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

func (c *TrainingOperator) GetReleaseStatus() *[]common.ComponentRelease {
	return &c.Status.Releases
}

func (c *TrainingOperator) SetReleaseStatus(releases []common.ComponentRelease) {
	c.Status.Releases = releases
}

// DSCTrainingOperator contains all the configuration exposed in DSC instance for TrainingOperator component
type DSCTrainingOperator struct {
	common.ManagementSpec `json:",inline"`
	// configuration fields common across components
	TrainingOperatorCommonSpec `json:",inline"`
}

// DSCTrainingOperatorStatus struct holds the status for the TrainingOperator component exposed in the DSC
type DSCTrainingOperatorStatus struct {
	common.ManagementSpec         `json:",inline"`
	*TrainingOperatorCommonStatus `json:",inline"`
}
