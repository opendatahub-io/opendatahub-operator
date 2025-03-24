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
	DashboardComponentName = "dashboard"
	// DashboardInstanceName the name of the Dashboard instance singleton.
	// value should match whats set in the XValidation below
	DashboardInstanceName = "default-" + DashboardComponentName
	DashboardKind         = "Dashboard"
)

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = (*Dashboard)(nil)

// DashboardCommonSpec spec defines the shared desired state of Dashboard
type DashboardCommonSpec struct {
	// dashboard spec exposed to DSC api
	common.DevFlagsSpec `json:",inline"`
	// dashboard spec exposed only to internal api
}

// DashboardSpec defines the desired state of Dashboard
type DashboardSpec struct {
	// dashboard spec exposed to DSC api
	DashboardCommonSpec `json:",inline"`
	// dashboard spec exposed only to internal api
}

// DashboardCommonStatus defines the shared observed state of Dashboard
type DashboardCommonStatus struct {
	URL string `json:"url,omitempty"`
}

// DashboardStatus defines the observed state of Dashboard
type DashboardStatus struct {
	common.Status         `json:",inline"`
	DashboardCommonStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-dashboard'",message="Dashboard name must be default-dashboard"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"
// +kubebuilder:printcolumn:name="URL",type=string,JSONPath=`.status.url`,description="URL"

// Dashboard is the Schema for the dashboards API
type Dashboard struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DashboardSpec   `json:"spec,omitempty"`
	Status DashboardStatus `json:"status,omitempty"`
}

func (c *Dashboard) GetDevFlags() *common.DevFlags {
	return c.Spec.DevFlags
}

func (c *Dashboard) GetStatus() *common.Status {
	return &c.Status.Status
}

func (c *Dashboard) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *Dashboard) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

// +kubebuilder:object:root=true

// DashboardList contains a list of Dashboard
type DashboardList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Dashboard `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Dashboard{}, &DashboardList{})
}

// DSCDashboard contains all the configuration exposed in DSC instance for Dashboard component
type DSCDashboard struct {
	// configuration fields common across components
	common.ManagementSpec `json:",inline"`
	// dashboard specific field
	DashboardCommonSpec `json:",inline"`
}

// DSCDashboardStatus contains the observed state of the Dashboard exposed in the DSC instance
type DSCDashboardStatus struct {
	common.ManagementSpec  `json:",inline"`
	*DashboardCommonStatus `json:",inline"`
}
