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

const (
	EKSKubernetesEngineKind         = "EKSKubernetesEngine"
	EKSKubernetesEngineInstanceName = "default-ekskubernetesengine"
)

// Check that the component implements common.PlatformObject.
var _ apicommon.PlatformObject = (*EKSKubernetesEngine)(nil)

// EKSKubernetesEngineSpec defines the desired state of EKSKubernetesEngine.
type EKSKubernetesEngineSpec struct {
	// Dependencies defines the dependency configurations for the EKS Kubernetes Engine.
	// +optional
	Dependencies common.Dependencies `json:"dependencies,omitempty"`
}

// EKSKubernetesEngineStatus defines the observed state of EKSKubernetesEngine.
type EKSKubernetesEngineStatus struct {
	apicommon.Status `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-ekskubernetesengine'",message="EKSKubernetesEngine name must be default-ekskubernetesengine"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"
// +kubebuilder:printcolumn:name="Deps Available",type=string,JSONPath=`.status.conditions[?(@.type=="DependenciesAvailable")].status`,description="DependenciesAvailable"

// EKSKubernetesEngine is the Schema for the ekskubernetesengines API.
// It represents the configuration for an Amazon Elastic Kubernetes Service (EKS) cluster.
type EKSKubernetesEngine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   EKSKubernetesEngineSpec   `json:"spec,omitempty"`
	Status EKSKubernetesEngineStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// EKSKubernetesEngineList contains a list of EKSKubernetesEngine.
type EKSKubernetesEngineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []EKSKubernetesEngine `json:"items"`
}

func (s *EKSKubernetesEngine) GetConditions() []apicommon.Condition {
	return s.Status.GetConditions()
}

func (s *EKSKubernetesEngine) GetStatus() *apicommon.Status {
	return &s.Status.Status
}

func (c *EKSKubernetesEngine) SetConditions(conditions []apicommon.Condition) {
	c.Status.SetConditions(conditions)
}

func init() {
	SchemeBuilder.Register(&EKSKubernetesEngine{}, &EKSKubernetesEngineList{})
}
