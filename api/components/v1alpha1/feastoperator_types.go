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
	// FeastOperatorName is the name of the new component
	//"feastoperator" is named to distinguish it from "feast," as it specifically refers to the operator responsible for deploying, managing, Feast feature store services and to avoid confusion with the core Feast components.
	FeastOperatorComponentName = "feastoperator"

	// FeastOperatorInstanceName is the singleton name for the FeastOperator instance
	// Value must match the validation rule defined below
	FeastOperatorInstanceName = "default-" + FeastOperatorComponentName

	// FeastOperatorKind represents the Kubernetes kind for FeastOperator
	FeastOperatorKind = "FeastOperator"
)

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = (*FeastOperator)(nil)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-feastoperator'",message="FeastOperator name must be 'default-feastoperator'"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// FeastOperator is the Schema for the FeastOperator API
type FeastOperator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FeastOperatorSpec   `json:"spec,omitempty"`
	Status FeastOperatorStatus `json:"status,omitempty"`
}

// FeastOperatorCommonSpec defines the common spec shared across APIs for FeastOperator
type FeastOperatorCommonSpec struct {
	// Spec fields exposed to the DSC API
	common.DevFlagsSpec `json:",inline"`
}

// FeastOperatorCommonStatus defines the shared observed state of FeastOperator
type FeastOperatorCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// FeastOperatorSpec defines the desired state of FeastOperator
type FeastOperatorSpec struct {
	FeastOperatorCommonSpec `json:",inline"`
}

// FeastOperatorStatus defines the observed state of FeastOperator
type FeastOperatorStatus struct {
	common.Status             `json:",inline"`
	FeastOperatorCommonStatus `json:",inline"`
}

// GetDevFlags retrieves the development flags from the spec
func (c *FeastOperator) GetDevFlags() *common.DevFlags {
	return c.Spec.DevFlags
}

// GetStatus retrieves the status of the FeastOperator component
func (f *FeastOperator) GetStatus() *common.Status {
	return &f.Status.Status
}

func (c *FeastOperator) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *FeastOperator) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

// +kubebuilder:object:root=true

// FeastOperatorList contains a list of FeastOperator objects
type FeastOperatorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FeastOperator `json:"items"`
}

// DSCFeastOperator defines the configuration exposed in the DSC instance for FeastOperator
type DSCFeastOperator struct {
	// Fields common across components
	common.ManagementSpec `json:",inline"`

	// FeastOperator-specific fields
	FeastOperatorCommonSpec `json:",inline"`
}

// DSCFeastOperatorStatus struct holds the status for the FeastOperator component exposed in the DSC
type DSCFeastOperatorStatus struct {
	common.ManagementSpec      `json:",inline"`
	*FeastOperatorCommonStatus `json:",inline"`
}

func init() {
	// Register the schema with the scheme builder
	SchemeBuilder.Register(&FeastOperator{}, &FeastOperatorList{})
}

func (f *FeastOperator) GetReleaseStatus() *[]common.ComponentRelease {
	return &f.Status.Releases
}

func (f *FeastOperator) SetReleaseStatus(releases []common.ComponentRelease) {
	f.Status.Releases = releases
}
