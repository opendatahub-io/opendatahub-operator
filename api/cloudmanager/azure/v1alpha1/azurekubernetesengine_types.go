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
	AzureKubernetesEngineKind         = "AzureKubernetesEngine"
	AzureKubernetesEngineInstanceName = "default-azurekubernetesengine"
)

// Check that the component implements common.PlatformObject.
var _ apicommon.PlatformObject = (*AzureKubernetesEngine)(nil)

// AzureKubernetesEngineSpec defines the desired state of AzureKubernetesEngine.
type AzureKubernetesEngineSpec struct {
	// Dependencies defines the dependency configurations for the Azure Kubernetes Engine.
	// +optional
	Dependencies common.Dependencies `json:"dependencies,omitempty"`
}

// AzureKubernetesEngineStatus defines the observed state of AzureKubernetesEngine.
type AzureKubernetesEngineStatus struct {
	apicommon.Status `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-azurekubernetesengine'",message="AzureKubernetesEngine name must be default-azurekubernetesengine"

// AzureKubernetesEngine is the Schema for the azurekubernetesengines API.
// It represents the configuration for an Azure Kubernetes Service (AKS) cluster.
type AzureKubernetesEngine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AzureKubernetesEngineSpec   `json:"spec,omitempty"`
	Status AzureKubernetesEngineStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// AzureKubernetesEngineList contains a list of AzureKubernetesEngine.
type AzureKubernetesEngineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AzureKubernetesEngine `json:"items"`
}

func (s *AzureKubernetesEngine) GetConditions() []apicommon.Condition {
	return s.Status.GetConditions()
}

func (s *AzureKubernetesEngine) GetStatus() *apicommon.Status {
	return &s.Status.Status
}

func (c *AzureKubernetesEngine) SetConditions(conditions []apicommon.Condition) {
	c.Status.SetConditions(conditions)
}

func init() {
	SchemeBuilder.Register(&AzureKubernetesEngine{}, &AzureKubernetesEngineList{})
}
