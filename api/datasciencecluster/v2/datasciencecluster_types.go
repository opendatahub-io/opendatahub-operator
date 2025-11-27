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

package v2

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DataScienceClusterSpec defines the desired state of the cluster.
type DataScienceClusterSpec struct {
	// Override and fine tune specific component configurations.
	Components Components `json:"components,omitempty"`
}

type Components struct {
	// Dashboard component configuration.
	Dashboard componentApi.DSCDashboard `json:"dashboard,omitempty"`

	// Workbenches component configuration.
	Workbenches componentApi.DSCWorkbenches `json:"workbenches,omitempty"`

	// AIPipelines component configuration.
	AIPipelines componentApi.DSCDataSciencePipelines `json:"aipipelines,omitempty"`

	// Kserve component configuration.
	// Only RawDeployment mode is supported.
	Kserve componentApi.DSCKserve `json:"kserve,omitempty"`

	// Kueue component configuration.
	Kueue componentApi.DSCKueue `json:"kueue,omitempty"`

	// Ray component configuration.
	Ray componentApi.DSCRay `json:"ray,omitempty"`

	// TrustyAI component configuration.
	TrustyAI componentApi.DSCTrustyAI `json:"trustyai,omitempty"`

	// ModelRegistry component configuration.
	ModelRegistry componentApi.DSCModelRegistry `json:"modelregistry,omitempty"`

	// Training Operator component configuration.
	TrainingOperator componentApi.DSCTrainingOperator `json:"trainingoperator,omitempty"`

	// Feast Operator component configuration.
	FeastOperator componentApi.DSCFeastOperator `json:"feastoperator,omitempty"`

	// LlamaStack Operator component configuration.
	LlamaStackOperator componentApi.DSCLlamaStackOperator `json:"llamastackoperator,omitempty"`

	// Trainer component configuration.
	Trainer componentApi.DSCTrainer `json:"trainer,omitempty"`
}

// ComponentsStatus defines the custom status of DataScienceCluster components.
type ComponentsStatus struct {
	// Dashboard component status.
	Dashboard componentApi.DSCDashboardStatus `json:"dashboard,omitempty"`

	// Workbenches component status.
	Workbenches componentApi.DSCWorkbenchesStatus `json:"workbenches,omitempty"`

	// AIPipelines component status.
	AIPipelines componentApi.DSCDataSciencePipelinesStatus `json:"aipipelines,omitempty"`

	// Kserve component status.
	Kserve componentApi.DSCKserveStatus `json:"kserve,omitempty"`

	// Kueue component status.
	Kueue componentApi.DSCKueueStatus `json:"kueue,omitempty"`

	// Ray component status.
	Ray componentApi.DSCRayStatus `json:"ray,omitempty"`

	// TrustyAI component status.
	TrustyAI componentApi.DSCTrustyAIStatus `json:"trustyai,omitempty"`

	// ModelRegistry component status.
	ModelRegistry componentApi.DSCModelRegistryStatus `json:"modelregistry,omitempty"`

	// Training Operator component status.
	TrainingOperator componentApi.DSCTrainingOperatorStatus `json:"trainingoperator,omitempty"`

	// Feast Operator component status.
	FeastOperator componentApi.DSCFeastOperatorStatus `json:"feastoperator,omitempty"`

	// LlamaStack Operator component status.
	LlamaStackOperator componentApi.DSCLlamaStackOperatorStatus `json:"llamastackoperator,omitempty"`

	// Trainer component status.
	Trainer componentApi.DSCTrainerStatus `json:"trainer,omitempty"`
}

// DataScienceClusterStatus defines the observed state of DataScienceCluster.
type DataScienceClusterStatus struct {
	common.Status `json:",inline"`

	// RelatedObjects is a list of objects created and maintained by this operator.
	// Object references will be added to this list after they have been created AND found in the cluster.
	// +optional
	RelatedObjects []corev1.ObjectReference `json:"relatedObjects,omitempty"`
	ErrorMessage   string                   `json:"errorMessage,omitempty"`

	// Expose component's specific status
	// +optional
	Components ComponentsStatus `json:"components"`

	// Version and release type
	Release common.Release `json:"release,omitempty"`
}

func (s *DataScienceClusterStatus) GetConditions() []common.Condition {
	return s.Conditions
}

func (s *DataScienceClusterStatus) SetConditions(conditions []common.Condition) {
	s.Conditions = append(conditions[:0:0], conditions...)
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:resource:scope=Cluster,shortName=dsc
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// DataScienceCluster is the Schema for the datascienceclusters API.
type DataScienceCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DataScienceClusterSpec   `json:"spec,omitempty"`
	Status DataScienceClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DataScienceClusterList contains a list of DataScienceCluster.
type DataScienceClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DataScienceCluster `json:"items"`
}

func (c *DataScienceCluster) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *DataScienceCluster) GetStatus() *common.Status {
	return &c.Status.Status
}

func (c *DataScienceCluster) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

func init() {
	SchemeBuilder.Register(&DataScienceCluster{}, &DataScienceClusterList{})
}
