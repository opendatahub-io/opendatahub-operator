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
	TrustyAIComponentName = "trustyai"
	// value should match whats set in the XValidation below
	TrustyAIInstanceName = "default-" + TrustyAIComponentName
	TrustyAIKind         = "TrustyAI"
)

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = (*TrustyAI)(nil)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-trustyai'",message="TrustyAI name must be default-trustyai"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// TrustyAI is the Schema for the trustyais API
type TrustyAI struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TrustyAISpec   `json:"spec,omitempty"`
	Status TrustyAIStatus `json:"status,omitempty"`
}

// TrustyAISpec defines the desired state of TrustyAI
type TrustyAISpec struct {
	TrustyAICommonSpec `json:",inline"`
}

// TrustyAIEvalSpec defines evaluation configuration for TrustyAI
type TrustyAIEvalSpec struct {
	// LMEval configuration for model evaluations
	LMEval TrustyAILMEvalSpec `json:"lmeval,omitempty"`
}

// TrustyAILMEvalSpec defines configuration for LMEval evaluations
type TrustyAILMEvalSpec struct {
	// PermitCodeExecution controls whether code execution is allowed during evaluations
	// +kubebuilder:default=false
	PermitCodeExecution bool `json:"permitCodeExecution,omitempty"`
	// PermitOnline controls whether online access is allowed during evaluations
	// +kubebuilder:default=false
	PermitOnline bool `json:"permitOnline,omitempty"`
}

type TrustyAICommonSpec struct {
	common.DevFlagsSpec `json:",inline"`
	// Eval configuration for TrustyAI evaluations
	Eval TrustyAIEvalSpec `json:"eval,omitempty"`
}

// TrustyAICommonStatus defines the shared observed state of TrustyAI
type TrustyAICommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// TrustyAIStatus defines the observed state of TrustyAI
type TrustyAIStatus struct {
	common.Status        `json:",inline"`
	TrustyAICommonStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// TrustyAIList contains a list of TrustyAI
type TrustyAIList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TrustyAI `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TrustyAI{}, &TrustyAIList{})
}

func (c *TrustyAI) GetDevFlags() *common.DevFlags {
	return c.Spec.DevFlags
}

func (c *TrustyAI) GetStatus() *common.Status {
	return &c.Status.Status
}

func (c *TrustyAI) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *TrustyAI) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

func (c *TrustyAI) GetReleaseStatus() *[]common.ComponentRelease { return &c.Status.Releases }

func (c *TrustyAI) SetReleaseStatus(releases []common.ComponentRelease) {
	c.Status.Releases = releases
}

// DSCTrustyAI contains all the configuration exposed in DSC instance for TrustyAI component
type DSCTrustyAI struct {
	common.ManagementSpec `json:",inline"`
	// configuration fields common across components
	TrustyAICommonSpec `json:",inline"`
}

// DSCTrustyAIStatus struct holds the status for the TrustyAI component exposed in the DSC
type DSCTrustyAIStatus struct {
	common.ManagementSpec `json:",inline"`
	*TrustyAICommonStatus `json:",inline"`
}
