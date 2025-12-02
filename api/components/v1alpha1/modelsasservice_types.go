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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

const (
	ModelsAsServiceComponentName = "modelsasservice"
	// value should match whats set in the XValidation below
	ModelsAsServiceInstanceName = "default-" + ModelsAsServiceComponentName
	ModelsAsServiceKind         = "ModelsAsService"
)

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = (*ModelsAsService)(nil)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-modelsasservice'",message="ModelsAsService name must be default-modelsasservice"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// ModelsAsService is the Schema for the modelsasservice API
type ModelsAsService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModelsAsServiceSpec   `json:"spec,omitempty"`
	Status ModelsAsServiceStatus `json:"status,omitempty"`
}

// ModelsAsServiceSpec defines the desired state of ModelsAsService
type ModelsAsServiceSpec struct {
	Gateway GatewaySpec `json:"gateway,omitempty"`
}

// GatewaySpec defines the reference to the global Gateway (Gw API) where
// models should be published to when exposed as services.
type GatewaySpec struct {
	// Namespace is the namespace name where the gateway.networking.k8s.io/v1/Gateway resource is.
	Namespace string `json:"namespace,omitempty"`

	// Name is the name of the gateway.networking.k8s.io/v1/Gateway resource.
	Name string `json:"name,omitempty"`
}

// ModelsAsServiceStatus defines the observed state of ModelsAsService
type ModelsAsServiceStatus struct {
	common.Status `json:",inline"`
}

// +kubebuilder:object:root=true
// ModelsAsServiceList contains a list of ModelsAsService
type ModelsAsServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ModelsAsService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ModelsAsService{}, &ModelsAsServiceList{})
}

func (c *ModelsAsService) GetStatus() *common.Status {
	return &c.Status.Status
}

func (c *ModelsAsService) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *ModelsAsService) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}
