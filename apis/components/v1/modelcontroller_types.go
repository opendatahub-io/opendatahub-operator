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

package v1

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/apis/common"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// shared by kserve and modelmeshserving
	// value should match whats set in the XValidation below
	ModelControllerInstanceName = "default-modelcontroller"
	ModelControllerKind         = "ModelController"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-modelcontroller'",message="ModelController name must be default-modelcontroller"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// ModelController is the Schema for the modelcontroller API, it is a shared component between kserve and modelmeshserving
type ModelController struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModelControllerSpec   `json:"spec,omitempty"`
	Status ModelControllerStatus `json:"status,omitempty"`
}

// ModelControllerSpec defines the desired state of ModelController
type ModelControllerSpec struct {
	common.DevFlagsSpec `json:",inline"`
	ModelMeshServing    operatorv1.ManagementState `json:"modelMeshServing,omitempty"`
	Kserve              operatorv1.ManagementState `json:"kserve,omitempty"`
}

// ModelControllerStatus defines the observed state of ModelController
type ModelControllerStatus struct {
	common.Status `json:",inline"`
}

// +kubebuilder:object:root=true
// ModelControllerList contains a list of ModelController
type ModelControllerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ModelController `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ModelController{}, &ModelControllerList{})
}

func (c *ModelController) GetDevFlags() *common.DevFlags { return nil }

func (c *ModelController) GetStatus() *common.Status {
	return &c.Status.Status
}
