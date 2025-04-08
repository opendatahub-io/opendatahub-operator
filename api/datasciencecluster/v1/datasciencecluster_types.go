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
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
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

	// ModelMeshServing component configuration.
	ModelMeshServing componentApi.DSCModelMeshServing `json:"modelmeshserving,omitempty"`

	// DataSciencePipeline component configuration.
	// Requires OpenShift Pipelines Operator to be installed before enable component
	DataSciencePipelines componentApi.DSCDataSciencePipelines `json:"datasciencepipelines,omitempty"`

	// Kserve component configuration.
	// Requires OpenShift Serverless and OpenShift Service Mesh Operators to be installed before enable component
	// Does not support enabled ModelMeshServing at the same time
	Kserve componentApi.DSCKserve `json:"kserve,omitempty"`

	// Kueue component configuration.
	Kueue componentApi.DSCKueue `json:"kueue,omitempty"`

	// CodeFlare component configuration.
	// If CodeFlare Operator has been installed in the cluster, it should be uninstalled first before enabling component.
	CodeFlare componentApi.DSCCodeFlare `json:"codeflare,omitempty"`

	// Ray component configuration.
	Ray componentApi.DSCRay `json:"ray,omitempty"`

	// TrustyAI component configuration.
	TrustyAI componentApi.DSCTrustyAI `json:"trustyai,omitempty"`

	// ModelRegistry component configuration.
	ModelRegistry componentApi.DSCModelRegistry `json:"modelregistry,omitempty"`

	// Training Operator component configuration.
	TrainingOperator componentApi.DSCTrainingOperator `json:"trainingoperator,omitempty"`
}

// ComponentsStatus defines the custom status of DataScienceCluster components.
type ComponentsStatus struct {
	// Dashboard component status.
	Dashboard componentApi.DSCDashboardStatus `json:"dashboard,omitempty"`

	// Workbenches component status.
	Workbenches componentApi.DSCWorkbenchesStatus `json:"workbenches,omitempty"`

	// ModelMeshServing component status.
	ModelMeshServing componentApi.DSCModelMeshServingStatus `json:"modelmeshserving,omitempty"`

	// DataSciencePipeline component status.
	DataSciencePipelines componentApi.DSCDataSciencePipelinesStatus `json:"datasciencepipelines,omitempty"`

	// Kserve component status.
	Kserve componentApi.DSCKserveStatus `json:"kserve,omitempty"`

	// Kueue component status.
	Kueue componentApi.DSCKueueStatus `json:"kueue,omitempty"`

	// CodeFlare component status.
	CodeFlare componentApi.DSCCodeFlareStatus `json:"codeflare,omitempty"`

	// Ray component status.
	Ray componentApi.DSCRayStatus `json:"ray,omitempty"`

	// TrustyAI component status.
	TrustyAI componentApi.DSCTrustyAIStatus `json:"trustyai,omitempty"`

	// ModelRegistry component status.
	ModelRegistry componentApi.DSCModelRegistryStatus `json:"modelregistry,omitempty"`

	// Training Operator component status.
	TrainingOperator componentApi.DSCTrainingOperatorStatus `json:"trainingoperator,omitempty"`
}

// DataScienceClusterStatus defines the observed state of DataScienceCluster.
type DataScienceClusterStatus struct {
	common.Status `json:",inline"`

	// RelatedObjects is a list of objects created and maintained by this operator.
	// Object references will be added to this list after they have been created AND found in the cluster.
	// +optional
	RelatedObjects []corev1.ObjectReference `json:"relatedObjects,omitempty"`
	ErrorMessage   string                   `json:"errorMessage,omitempty"`

	// List of components with status if installed or not
	InstalledComponents map[string]bool `json:"installedComponents,omitempty"`

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
// +kubebuilder:resource:scope=Cluster,shortName=dsc
// +kubebuilder:storageversion
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
