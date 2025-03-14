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
	ModelMeshServingComponentName = "modelmeshserving"
	// value should match whats set in the XValidation below
	ModelMeshServingInstanceName = "default-" + ModelMeshServingComponentName
	ModelMeshServingKind         = "ModelMeshServing"
)

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = (*ModelMeshServing)(nil)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-modelmeshserving'",message="ModelMeshServing name must be default-modelmeshserving"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// ModelMeshServing is the Schema for the modelmeshservings API
type ModelMeshServing struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModelMeshServingSpec   `json:"spec,omitempty"`
	Status ModelMeshServingStatus `json:"status,omitempty"`
}

// ModelMeshServingSpec defines the desired state of ModelMeshServing
type ModelMeshServingSpec struct {
	ModelMeshServingCommonSpec `json:",inline"`
}

type ModelMeshServingCommonSpec struct {
	common.DevFlagsSpec `json:",inline"`
}

// ModelMeshServingCommonStatus defines the shared observed state of ModelMeshServing
type ModelMeshServingCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// ModelMeshServingStatus defines the observed state of ModelMeshServing
type ModelMeshServingStatus struct {
	common.Status                `json:",inline"`
	ModelMeshServingCommonStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// ModelMeshServingList contains a list of ModelMeshServing
type ModelMeshServingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ModelMeshServing `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ModelMeshServing{}, &ModelMeshServingList{})
}

func (c *ModelMeshServing) GetDevFlags() *common.DevFlags {
	return c.Spec.DevFlags
}

func (c *ModelMeshServing) GetStatus() *common.Status {
	return &c.Status.Status
}

func (c *ModelMeshServing) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *ModelMeshServing) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

func (c *ModelMeshServing) GetReleaseStatus() *[]common.ComponentRelease {
	return &c.Status.Releases
}

func (c *ModelMeshServing) SetReleaseStatus(releases []common.ComponentRelease) {
	c.Status.Releases = releases
}

// DSCModelMeshServing contains all the configuration exposed in DSC instance for ModelMeshServing component
type DSCModelMeshServing struct {
	common.ManagementSpec `json:",inline"`
	// configuration fields common across components
	ModelMeshServingCommonSpec `json:",inline"`
}

// DSCModelMeshServingStatus contains the observed state of the ModelMeshServing exposed in the DSC instance
type DSCModelMeshServingStatus struct {
	common.ManagementSpec         `json:",inline"`
	*ModelMeshServingCommonStatus `json:",inline"`
}
