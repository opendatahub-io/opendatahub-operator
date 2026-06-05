/*
Copyright 2026.

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
	// Component name
	BatchGatewayComponentName = "batchgateway"

	// BatchGatewayInstanceName is the name of the component instance singleton
	// value should match what is set in the kubebuilder markers for XValidation defined below
	BatchGatewayInstanceName = "default-" + BatchGatewayComponentName

	// Kubernetes kind of the component
	BatchGatewayKind = "BatchGateway"
)

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = (*BatchGateway)(nil)

type BatchGatewayCommonSpec struct {
	// TODO: Add BatchGateway specific configuration fields
}

// BatchGatewaySpec defines the desired state of BatchGateway
type BatchGatewaySpec struct {
	BatchGatewayCommonSpec `json:",inline"`
}

// BatchGatewayCommonStatus defines the shared observed state
type BatchGatewayCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// BatchGatewayStatus defines the observed state
type BatchGatewayStatus struct {
	common.Status            `json:",inline"`
	BatchGatewayCommonStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-batchgateway'",message="BatchGateway name must be default-batchgateway"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// BatchGateway is the Schema for the batchgateways API
type BatchGateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BatchGatewaySpec   `json:"spec,omitempty"`
	Status BatchGatewayStatus `json:"status,omitempty"`
}

// GetStatus retrieves the status
func (c *BatchGateway) GetStatus() *common.Status {
	return &c.Status.Status
}

func (c *BatchGateway) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *BatchGateway) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

func (c *BatchGateway) GetReleaseStatus() *[]common.ComponentRelease {
	return &c.Status.Releases
}

func (c *BatchGateway) SetReleaseStatus(releases []common.ComponentRelease) {
	c.Status.Releases = releases
}

// +kubebuilder:object:root=true

// BatchGatewayList contains a list of BatchGateway
type BatchGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BatchGateway `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BatchGateway{}, &BatchGatewayList{})
}

// DSCBatchGateway contains all the configuration exposed in DSC instance
type DSCBatchGateway struct {
	common.ManagementSpec  `json:",inline"`
	BatchGatewayCommonSpec `json:",inline"`
}

// DSCBatchGatewayStatus contains the observed state exposed in the DSC
type DSCBatchGatewayStatus struct {
	common.ManagementSpec     `json:",inline"`
	*BatchGatewayCommonStatus `json:",inline"`
}
