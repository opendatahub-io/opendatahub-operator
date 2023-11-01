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
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/components/codeflare"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/datasciencepipelines"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/kserve"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/modelmeshserving"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/ray"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/trustyai"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/workbenches"
)

// DataScienceCluster defines the desired state of the cluster.
type DataScienceClusterSpec struct {
	// Override and fine tune specific component configurations.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=1
	Components Components `json:"components,omitempty"`
}

type Components struct {
	// Dashboard component configuration.
	Dashboard dashboard.Dashboard `json:"dashboard,omitempty"`

	// Workbenches component configuration.
	Workbenches workbenches.Workbenches `json:"workbenches,omitempty"`

	// ModelMeshServing component configuration.
	// Require CodeFlare Operator to be installed before enable component
	// Does not support enabled Kserve at the same time
	ModelMeshServing modelmeshserving.ModelMeshServing `json:"modelmeshserving,omitempty"`

	// DataServicePipeline component configuration.
	// Require OpenShift Pipelines Operator to be installed before enable component
	DataSciencePipelines datasciencepipelines.DataSciencePipelines `json:"datasciencepipelines,omitempty"`

	// Kserve component configuration.
	// Require OpenShift Serverless and OpenShift Service Mesh Operators to be installed before enable component
	// Does not support enabled ModelMeshServing at the same time
	Kserve kserve.Kserve `json:"kserve,omitempty"`

	// CodeFlare component configuration.
	// Require CodeFlare Operator to be installed before enable component
	CodeFlare codeflare.CodeFlare `json:"codeflare,omitempty"`

	// Ray component configuration.
	// Require CodeFlare Operator to be installed before enable component
	Ray ray.Ray `json:"ray,omitempty"`

	// TrustyAI component configuration.
	TrustyAI trustyai.TrustyAI `json:"trustyai,omitempty"`
}

// DataScienceClusterStatus defines the observed state of DataScienceCluster.
type DataScienceClusterStatus struct {
	// Phase describes the Phase of DataScienceCluster reconciliation state
	// This is used by OLM UI to provide status information to the user
	Phase string `json:"phase,omitempty"`

	// Conditions describes the state of the DataScienceCluster resource.
	// +optional
	Conditions []conditionsv1.Condition `json:"conditions,omitempty"`

	// RelatedObjects is a list of objects created and maintained by this operator.
	// Object references will be added to this list after they have been created AND found in the cluster.
	// +optional
	RelatedObjects []corev1.ObjectReference `json:"relatedObjects,omitempty"`
	ErrorMessage   string                   `json:"errorMessage,omitempty"`

	// List of components with status if installed or not
	InstalledComponents map[string]bool `json:"installedComponents,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster,shortName=dsc
//+kubebuilder:storageversion

// DataScienceCluster is the Schema for the datascienceclusters API.
type DataScienceCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DataScienceClusterSpec   `json:"spec,omitempty"`
	Status DataScienceClusterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// DataScienceClusterList contains a list of DataScienceCluster.
type DataScienceClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DataScienceCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DataScienceCluster{}, &DataScienceClusterList{})
}
