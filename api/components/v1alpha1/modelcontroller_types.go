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
	ModelControllerComponentName = "modelcontroller"
	// value should match whats set in the XValidation below
	ModelControllerInstanceName = "default-" + ModelControllerComponentName
	ModelControllerKind         = "ModelController"
)

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = (*ModelController)(nil)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-modelcontroller'",message="ModelController name must be default-modelcontroller"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"
// +kubebuilder:printcolumn:name="URI",type=string,JSONPath=`.status.URI`,description="devFlag's URI used to download"

// ModelController is the Schema for the modelcontroller API
type ModelController struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModelControllerSpec   `json:"spec,omitempty"`
	Status ModelControllerStatus `json:"status,omitempty"`
}

// ModelControllerSpec defines the desired state of ModelController
type ModelControllerSpec struct {
	Kserve        *ModelControllerKerveSpec `json:"kserve,omitempty"`
	ModelRegistry *ModelControllerMRSpec    `json:"modelRegistry,omitempty"`
}

// a mini version of the DSCKserve only keeps management and NIM spec
type ModelControllerKerveSpec struct {
	ManagementState operatorv1.ManagementState `json:"managementState,omitempty"`
	NIM             NimSpec                    `json:"nim,omitempty"`
}

// a mini version of the DSCModelMeshServing only keeps management spec
type ModelControllerMMSpec struct {
	ManagementState operatorv1.ManagementState `json:"managementState,omitempty"`
}

type ModelControllerMRSpec struct {
	ManagementState operatorv1.ManagementState `json:"managementState,omitempty"`
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

func (c *ModelController) GetStatus() *common.Status {
	return &c.Status.Status
}

func (c *ModelController) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *ModelController) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}
