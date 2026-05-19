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
	AWSKubernetesEngineKind         = "AWSKubernetesEngine"
	AWSKubernetesEngineInstanceName = "default-awskubernetesengine"
)

// Check that the component implements common.KubernetesEngineInstance.
var _ common.KubernetesEngineInstance = (*AWSKubernetesEngine)(nil)

// AWSKubernetesEngineSpec defines the desired state of AWSKubernetesEngine.
type AWSKubernetesEngineSpec struct {
	// Dependencies defines the dependency configurations for the AWS Kubernetes Engine.
	// +optional
	Dependencies common.Dependencies `json:"dependencies,omitempty"`
}

// AWSKubernetesEngineStatus defines the observed state of AWSKubernetesEngine.
type AWSKubernetesEngineStatus struct {
	apicommon.Status `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-awskubernetesengine'",message="AWSKubernetesEngine name must be default-awskubernetesengine"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"
// +kubebuilder:printcolumn:name="Deps Available",type=string,JSONPath=`.status.conditions[?(@.type=="DependenciesAvailable")].status`,description="DependenciesAvailable"

// AWSKubernetesEngine is the Schema for the awskubernetesengines API.
// It represents the configuration for an AWS Kubernetes Service cluster.
type AWSKubernetesEngine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AWSKubernetesEngineSpec   `json:"spec,omitempty"`
	Status AWSKubernetesEngineStatus `json:"status,omitempty"`
}

func (e *AWSKubernetesEngine) GetDependencies() common.Dependencies {
	return e.Spec.Dependencies
}

// +kubebuilder:object:root=true

// AWSKubernetesEngineList contains a list of AWSKubernetesEngine.
type AWSKubernetesEngineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AWSKubernetesEngine `json:"items"`
}

func (s *AWSKubernetesEngine) GetConditions() []apicommon.Condition {
	return s.Status.GetConditions()
}

func (s *AWSKubernetesEngine) GetStatus() *apicommon.Status {
	return &s.Status.Status
}

func (c *AWSKubernetesEngine) SetConditions(conditions []apicommon.Condition) {
	c.Status.SetConditions(conditions)
}

func init() {
	SchemeBuilder.Register(&AWSKubernetesEngine{}, &AWSKubernetesEngineList{})
}
