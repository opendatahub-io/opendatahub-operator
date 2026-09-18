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
)

const (
	DataSciencePipelinesComponentName = "datasciencepipelines"
	// AIPipelinesComponentName is the platform module name used by the v2 DSC API.
	AIPipelinesComponentName = "aipipelines"
	// AIPipelinesInstanceName is the singleton name of the out-of-tree module CR.
	AIPipelinesInstanceName = "default-" + AIPipelinesComponentName
	// AIPipelinesKind is the user-facing name for DataSciencePipelines in v2
	AIPipelinesKind = "AIPipelines"
)

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
	ArgoWorkflowsControllers *ArgoWorkflowsControllersSpec `json:"argoWorkflowsControllers,omitempty"`
}

// DataSciencePipelinesCommonStatus defines the shared observed state of DataSciencePipelines
type DataSciencePipelinesCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
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
