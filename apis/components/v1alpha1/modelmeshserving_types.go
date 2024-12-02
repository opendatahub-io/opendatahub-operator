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
	"github.com/opendatahub-io/opendatahub-operator/v2/apis/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ModelMeshServingSpec defines the desired state of ModelMeshServing
type ModelMeshServingSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of ModelMeshServing. Edit modelmeshserving_types.go to remove/update
	Foo string `json:"foo,omitempty"`
}

// ModelMeshServingStatus defines the observed state of ModelMeshServing
type ModelMeshServingStatus struct {
	common.Status `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// ModelMeshServing is the Schema for the modelmeshservings API
type ModelMeshServing struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModelMeshServingSpec   `json:"spec,omitempty"`
	Status ModelMeshServingStatus `json:"status,omitempty"`
}

func (c *ModelMeshServing) GetDevFlags() *common.DevFlags {
	return nil
}

func (c *ModelMeshServing) GetStatus() *common.Status {
	return &c.Status.Status
}

// +kubebuilder:object:root=true

// ModelMeshServingList contains a list of ModelMeshServing
type ModelMeshServingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ModelMeshServing `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ModelMeshServing{}, &ModelMeshServingList{})
}
