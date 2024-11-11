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

package v1

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/apis/components"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	TrustyAIComponentName = "trustyai"
	// value should match whats set in the XValidation below
	TrustyAIInstanceName = "default-trustyai"
	TrustyAIKind         = "TrustyAI"
)

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

type TrustyAICommonSpec struct {
	components.DevFlagsSpec `json:",inline"`
}

// TrustyAIStatus defines the observed state of TrustyAI
type TrustyAIStatus struct {
	components.Status `json:",inline"`
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

func (c *TrustyAI) GetDevFlags() *components.DevFlags {
	return c.Spec.DevFlags
}
func (c *TrustyAI) GetStatus() *components.Status {
	return &c.Status.Status
}

// DSCTrustyAI contains all the configuration exposed in DSC instance for TrustyAI component
type DSCTrustyAI struct {
	components.ManagementSpec `json:",inline"`
	// configuration fields common across components
	TrustyAICommonSpec `json:",inline"`
}
