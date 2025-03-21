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
	CodeFlareComponentName = "codeflare"
	// value should match whats set in the XValidation below
	CodeFlareInstanceName = "default-" + CodeFlareComponentName
	CodeFlareKind         = "CodeFlare"
)

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = (*CodeFlare)(nil)

// CodeFlareCommonStatus defines the shared observed state of CodeFlare
type CodeFlareCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// CodeFlareStatus defines the observed state of CodeFlare
type CodeFlareStatus struct {
	common.Status         `json:",inline"`
	CodeFlareCommonStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-codeflare'",message="CodeFlare name must be default-codeflare"

// CodeFlare is the Schema for the codeflares API
type CodeFlare struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CodeFlareSpec   `json:"spec,omitempty"`
	Status CodeFlareStatus `json:"status,omitempty"`
}

type CodeFlareSpec struct {
	CodeFlareCommonSpec `json:",inline"`
}

type CodeFlareCommonSpec struct {
	common.DevFlagsSpec `json:",inline"`
}

func (c *CodeFlare) GetDevFlags() *common.DevFlags {
	return c.Spec.DevFlags
}

func (c *CodeFlare) GetStatus() *common.Status {
	return &c.Status.Status
}

func (c *CodeFlare) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *CodeFlare) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

func (c *CodeFlare) GetReleaseStatus() *[]common.ComponentRelease { return &c.Status.Releases }

func (c *CodeFlare) SetReleaseStatus(releases []common.ComponentRelease) {
	c.Status.Releases = releases
}

func init() {
	SchemeBuilder.Register(&CodeFlare{}, &CodeFlareList{})
}

// +kubebuilder:object:root=true

// CodeFlareList contains a list of CodeFlare
type CodeFlareList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CodeFlare `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CodeFlare{}, &CodeFlareList{})
}

type DSCCodeFlare struct {
	common.ManagementSpec `json:",inline"`
	CodeFlareCommonSpec   `json:",inline"`
}

// DSCCodeFlareStatus contains the observed state of the CodeFlare exposed in the DSC instance
type DSCCodeFlareStatus struct {
	common.ManagementSpec  `json:",inline"`
	*CodeFlareCommonStatus `json:",inline"`
}
