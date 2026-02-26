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
	"github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"
	apicommon "github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Check that the component implements common.PlatformObject.
var _ apicommon.PlatformObject = (*CoreWeaveKubernetesEngine)(nil)

// CoreWeaveKubernetesEngineSpec defines the desired state of CoreWeaveKubernetesEngine.
type CoreWeaveKubernetesEngineSpec struct {
	// Dependencies defines the dependency configurations for the CoreWeave Kubernetes Engine.
	// +optional
	Dependencies common.Dependencies `json:"dependencies,omitempty"`
}

// CoreWeaveKubernetesEngineStatus defines the observed state of CoreWeaveKubernetesEngine.
type CoreWeaveKubernetesEngineStatus struct {
	apicommon.Status `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster

// CoreWeaveKubernetesEngine is the Schema for the CoreWeaveKubernetesEngines API.
// It represents the configuration for an Azure Kubernetes Service (AKS) cluster.
type CoreWeaveKubernetesEngine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CoreWeaveKubernetesEngineSpec   `json:"spec,omitempty"`
	Status CoreWeaveKubernetesEngineStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CoreWeaveKubernetesEngineList contains a list of CoreWeaveKubernetesEngine.
type CoreWeaveKubernetesEngineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CoreWeaveKubernetesEngine `json:"items"`
}

func (s *CoreWeaveKubernetesEngine) GetConditions() []apicommon.Condition {
	return s.Status.GetConditions()
}

func (s *CoreWeaveKubernetesEngine) GetStatus() *apicommon.Status {
	return &s.Status.Status
}

func (c *CoreWeaveKubernetesEngine) SetConditions(conditions []apicommon.Condition) {
	c.Status.SetConditions(conditions)
}

func init() {
	SchemeBuilder.Register(&CoreWeaveKubernetesEngine{}, &CoreWeaveKubernetesEngineList{})
}
