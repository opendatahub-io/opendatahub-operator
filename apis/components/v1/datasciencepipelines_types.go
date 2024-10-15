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
	"github.com/opendatahub-io/opendatahub-operator/v2/apis/components"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// DataSciencePipelinesSpec defines the desired state of DataSciencePipelines
type DataSciencePipelinesSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of DataSciencePipelines. Edit datasciencepipelines_types.go to remove/update
	Foo string `json:"foo,omitempty"`
}

// DataSciencePipelinesStatus defines the observed state of DataSciencePipelines
type DataSciencePipelinesStatus struct {
	components.Status `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// DataSciencePipelines is the Schema for the datasciencepipelines API
type DataSciencePipelines struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DataSciencePipelinesSpec   `json:"spec,omitempty"`
	Status DataSciencePipelinesStatus `json:"status,omitempty"`
}

func (c *DataSciencePipelines) GetDevFlags() *components.DevFlags {
	return nil
}

func (c *DataSciencePipelines) GetStatus() *components.Status {
	return &c.Status.Status
}

// +kubebuilder:object:root=true

// DataSciencePipelinesList contains a list of DataSciencePipelines
type DataSciencePipelinesList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DataSciencePipelines `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DataSciencePipelines{}, &DataSciencePipelinesList{})
}
