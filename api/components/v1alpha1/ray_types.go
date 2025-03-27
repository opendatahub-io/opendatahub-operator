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
	RayComponentName = "ray"
	// value should match whats set in the XValidation below
	RayInstanceName = "default-" + RayComponentName
	RayKind         = "Ray"
)

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = (*Ray)(nil)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-ray'",message="Ray name must be default-ray"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// Ray is the Schema for the rays API
type Ray struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RaySpec   `json:"spec,omitempty"`
	Status RayStatus `json:"status,omitempty"`
}

// RaySpec defines the desired state of Ray
type RaySpec struct {
	RayCommonSpec `json:",inline"`
}

type RayCommonSpec struct {
	common.DevFlagsSpec `json:",inline"`
}

// RayCommonStatus defines the shared observed state of Ray
type RayCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// RayStatus defines the observed state of Ray
type RayStatus struct {
	common.Status   `json:",inline"`
	RayCommonStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// RayList contains a list of Ray
type RayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Ray `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Ray{}, &RayList{})
}

func (c *Ray) GetDevFlags() *common.DevFlags {
	return c.Spec.DevFlags
}

func (c *Ray) GetStatus() *common.Status {
	return &c.Status.Status
}

func (c *Ray) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *Ray) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

func (c *Ray) GetReleaseStatus() *[]common.ComponentRelease { return &c.Status.Releases }

func (c *Ray) SetReleaseStatus(releases []common.ComponentRelease) {
	c.Status.Releases = releases
}

// DSCRay contains all the configuration exposed in DSC instance for Ray component
type DSCRay struct {
	common.ManagementSpec `json:",inline"`
	// configuration fields common across components
	RayCommonSpec `json:",inline"`
}

// DSCRayStatus struct holds the status for the Ray component exposed in the DSC
type DSCRayStatus struct {
	common.ManagementSpec `json:",inline"`
	*RayCommonStatus      `json:",inline"`
}
