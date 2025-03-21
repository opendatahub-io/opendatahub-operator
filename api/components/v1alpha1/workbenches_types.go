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
	WorkbenchesComponentName = "workbenches"
	// WorkbenchesInstanceName the name of the Workbenches instance singleton.
	// value should match what is set in the XValidation below.
	WorkbenchesInstanceName = "default-" + WorkbenchesComponentName
	WorkbenchesKind         = "Workbenches"
)

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = (*Workbenches)(nil)

type WorkbenchesCommonSpec struct {
	// workbenches spec exposed to DSC api
	common.DevFlagsSpec `json:",inline"`
	// workbenches spec exposed only to internal api
}

// WorkbenchesSpec defines the desired state of Workbenches
type WorkbenchesSpec struct {
	// workbenches spec exposed to DSC api
	WorkbenchesCommonSpec `json:",inline"`
	// workbenches spec exposed only to internal api
}

// WorkbenchesCommonStatus defines the shared observed state of Workbenches
type WorkbenchesCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// WorkbenchesStatus defines the observed state of Workbenches
type WorkbenchesStatus struct {
	common.Status           `json:",inline"`
	WorkbenchesCommonStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-workbenches'",message="Workbenches name must be default-workbenches"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// Workbenches is the Schema for the workbenches API
type Workbenches struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkbenchesSpec   `json:"spec,omitempty"`
	Status WorkbenchesStatus `json:"status,omitempty"`
}

func (c *Workbenches) GetDevFlags() *common.DevFlags {
	return c.Spec.DevFlags
}

func (c *Workbenches) GetStatus() *common.Status {
	return &c.Status.Status
}

func (c *Workbenches) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *Workbenches) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

func (c *Workbenches) GetReleaseStatus() *[]common.ComponentRelease { return &c.Status.Releases }

func (c *Workbenches) SetReleaseStatus(releases []common.ComponentRelease) {
	c.Status.Releases = releases
}

// +kubebuilder:object:root=true

// WorkbenchesList contains a list of Workbenches
type WorkbenchesList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Workbenches `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Workbenches{}, &WorkbenchesList{})
}

// DSCWorkbenches contains all the configuration exposed in DSC instance for Workbenches component
type DSCWorkbenches struct {
	// configuration fields common across components
	common.ManagementSpec `json:",inline"`
	// workbenches specific field
	WorkbenchesCommonSpec `json:",inline"`
}

// DSCWorkbenchesStatus struct holds the status for the Workbenches component exposed in the DSC
type DSCWorkbenchesStatus struct {
	common.ManagementSpec    `json:",inline"`
	*WorkbenchesCommonStatus `json:",inline"`
}
