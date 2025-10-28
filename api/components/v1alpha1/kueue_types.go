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
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	KueueComponentName = "kueue"
	// value should match whats set in the XValidation below
	KueueInstanceName = "default-" + KueueComponentName
	KueueKind         = "Kueue"
)

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = (*Kueue)(nil)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-kueue'",message="Kueue name must be default-kueue"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// Kueue is the Schema for the kueues API
type Kueue struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KueueSpec   `json:"spec,omitempty"`
	Status KueueStatus `json:"status,omitempty"`
}

// KueueSpec defines the desired state of Kueue
type KueueSpec struct {
	KueueManagementSpec   `json:",inline"`
	KueueCommonSpec       `json:",inline"`
	KueueDefaultQueueSpec `json:",inline"`
}

type KueueCommonSpec struct{}

// KueueCommonStatus defines the shared observed state of Kueue
type KueueCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// KueueStatus defines the observed state of Kueue
type KueueStatus struct {
	common.Status     `json:",inline"`
	KueueCommonStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// KueueList contains a list of Kueue
type KueueList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Kueue `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Kueue{}, &KueueList{})
}

func (c *Kueue) GetStatus() *common.Status {
	return &c.Status.Status
}

func (c *Kueue) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *Kueue) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

func (c *Kueue) GetReleaseStatus() *[]common.ComponentRelease { return &c.Status.Releases }

func (c *Kueue) SetReleaseStatus(releases []common.ComponentRelease) {
	c.Status.Releases = releases
}

// KueueManagementSpec struct defines the component's management configuration.
// +kubebuilder:object:generate=true
type KueueManagementSpec struct {
	// Set to one of the following values:
	//
	// - "Unmanaged" : the operator will not deploy or manage the component's lifecycle, but may create supporting configuration resources.
	//
	// - "Removed"   : the operator is actively managing the component and will not install it,
	//                 or if it is installed, the operator will try to remove it
	//
	// +kubebuilder:validation:Enum=Unmanaged;Removed
	ManagementState operatorv1.ManagementState `json:"managementState,omitempty"`
}

// +kubebuilder:object:generate=true
type KueueDefaultQueueSpec struct {
	// Configures the automatically created, in the managed namespaces, local queue name.
	// +kubebuilder:default=default
	DefaultLocalQueueName string `json:"defaultLocalQueueName,omitempty"`
	// Configures the automatically created cluster queue name.
	// +kubebuilder:default=default
	DefaultClusterQueueName string `json:"defaultClusterQueueName,omitempty"`
}

// DSCKueue contains all the configuration exposed in DSC instance for Kueue component
type DSCKueue struct {
	KueueManagementSpec `json:",inline"`
	// configuration fields common across components
	KueueCommonSpec       `json:",inline"`
	KueueDefaultQueueSpec `json:",inline"`
}

// DSCKueueStatus contains the observed state of the Kueue exposed in the DSC instance
type DSCKueueStatus struct {
	KueueManagementSpec `json:",inline"`
	*KueueCommonStatus  `json:",inline"`
}
