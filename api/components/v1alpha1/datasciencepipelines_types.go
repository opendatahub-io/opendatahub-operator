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
	DataSciencePipelinesComponentName = "datasciencepipelines"
	// value should match whats set in the XValidation below
	DataSciencePipelinesInstanceName = "default-" + DataSciencePipelinesComponentName
	DataSciencePipelinesKind         = "DataSciencePipelines"
)

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = (*DataSciencePipelines)(nil)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-datasciencepipelines'",message="DataSciencePipelines name must be default-datasciencepipelines"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// DataSciencePipelines is the Schema for the datasciencepipelines API
type DataSciencePipelines struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DataSciencePipelinesSpec   `json:"spec,omitempty"`
	Status DataSciencePipelinesStatus `json:"status,omitempty"`
}

// DataSciencePipelinesSpec defines the desired state of DataSciencePipelines
type DataSciencePipelinesSpec struct {
	DataSciencePipelinesCommonSpec `json:",inline"`
}

type ArgoWorkflowsControllersSpec struct {
	// Set to one of the following values:
	//
	// - "Managed" : the operator is actively managing the bundled Argo Workflows controllers.
	//               It will only upgrade the Argo Workflows controllers if it is safe to do so. This is the default
	//               behavior.
	//
	// - "Removed" : the operator is not managing the bundled Argo Workflows controllers and will not install it.
	//               If it is installed, the operator will remove it but will not remove other Argo Workflows
	//               installations.
	//
	// +kubebuilder:validation:Enum=Managed;Removed
	// +kubebuilder:default=Managed
	ManagementState operatorv1.ManagementState `json:"managementState,omitempty"`
}

type DataSciencePipelinesCommonSpec struct {
	common.DevFlagsSpec      `json:",inline"`
	ArgoWorkflowsControllers *ArgoWorkflowsControllersSpec `json:"argoWorkflowsControllers,omitempty"`
}

// DataSciencePipelinesCommonStatus defines the shared observed state of DataSciencePipelines
type DataSciencePipelinesCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// DataSciencePipelinesStatus defines the observed state of DataSciencePipelines
type DataSciencePipelinesStatus struct {
	common.Status                    `json:",inline"`
	DataSciencePipelinesCommonStatus `json:",inline"`
}

// GetDevFlags returns the component's development flags configuration.
// May return nil if DevFlagsSpec is not set. Callers must nil-check the result
// to avoid null pointer exceptions in reconciler code.
func (c *DataSciencePipelines) GetDevFlags() *common.DevFlags {
	return c.Spec.DevFlags
}

func (c *DataSciencePipelines) GetStatus() *common.Status {
	return &c.Status.Status
}

func (c *DataSciencePipelines) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *DataSciencePipelines) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

func (c *DataSciencePipelines) GetReleaseStatus() *[]common.ComponentRelease {
	return &c.Status.Releases
}

func (c *DataSciencePipelines) SetReleaseStatus(releases []common.ComponentRelease) {
	c.Status.Releases = releases
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

// DSCDataSciencePipelines contains all the configuration exposed in DSC instance for DataSciencePipelines component
type DSCDataSciencePipelines struct {
	// configuration fields common across components
	common.ManagementSpec `json:",inline"`
	// datasciencepipelines specific field
	DataSciencePipelinesCommonSpec `json:",inline"`
}

// DSCDataSciencePipelinesStatus contains the observed state of the DataSciencePipelines exposed in the DSC instance
type DSCDataSciencePipelinesStatus struct {
	common.ManagementSpec             `json:",inline"`
	*DataSciencePipelinesCommonStatus `json:",inline"`
}
