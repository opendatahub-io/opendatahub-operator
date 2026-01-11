/*
Copyright 2025.

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
	TrainerComponentName = "trainer"
	// value should match whats set in the XValidation below
	TrainerInstanceName = "default-" + TrainerComponentName
	TrainerKind         = "Trainer"
)

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = (*Trainer)(nil)

// NOTE: json tags are required. Any new fields you add must have json tags for the fields to be serialized.

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-trainer'",message="Trainer name must be default-trainer"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// Trainer is the Schema for the trainers API
type Trainer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TrainerSpec   `json:"spec,omitempty"`
	Status TrainerStatus `json:"status,omitempty"`
}

// TrainerSpec defines the desired state of Trainer
type TrainerSpec struct {
	TrainerCommonSpec `json:",inline"`
}

type TrainerCommonSpec struct{}

// TrainerCommonStatus defines the shared observed state of Trainer
type TrainerCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// TrainerStatus defines the observed state of Trainer
type TrainerStatus struct {
	common.Status       `json:",inline"`
	TrainerCommonStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// TrainerList contains a list of Trainer
type TrainerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Trainer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Trainer{}, &TrainerList{})
}

func (c *Trainer) GetStatus() *common.Status {
	return &c.Status.Status
}

func (c *Trainer) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *Trainer) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

func (c *Trainer) GetReleaseStatus() *[]common.ComponentRelease {
	return &c.Status.Releases
}

func (c *Trainer) SetReleaseStatus(releases []common.ComponentRelease) {
	c.Status.Releases = releases
}

// DSCTrainer contains all the configuration exposed in DSC instance for Trainer component
type DSCTrainer struct {
	common.ManagementSpec `json:",inline"`
	// configuration fields common across components
	TrainerCommonSpec `json:",inline"`
}

// DSCTrainerStatus struct holds the status for the Trainer component exposed in the DSC
type DSCTrainerStatus struct {
	common.ManagementSpec `json:",inline"`
	*TrainerCommonStatus  `json:",inline"`
}
