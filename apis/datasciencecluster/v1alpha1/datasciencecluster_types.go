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
	"github.com/opendatahub-io/opendatahub-operator/components/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/components/datasciencepipelines"
	"github.com/opendatahub-io/opendatahub-operator/components/modelmeshserving"
	"github.com/opendatahub-io/opendatahub-operator/components/workbenches"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DataScienceClusterSpec defines the desired state of DataScienceCluster
type DataScienceClusterSpec struct {
	// A profile sets the default components and configuration to install for a given
	// use case. The profile configuration can still be overriden by the user on a per
	// component basis. If not defined, the 'core' profile is used.
	Profile ProfileValue `json:"profile,omitempty"`

	// Components are used to override and fine tune specific component configurations.
	Components Components `json:"components,omitempty"`
}

// Valid values for the Profile in the DataScienceCluster are as follows:
// - core: all core components are installed
// - serving: only serving components are installed
// - training: only training components are installed
// - workbench: only workbench components are installed
// +kubebuilder:validation:Enum=core;serving;training;workbench
type ProfileValue string

type Components struct {
	// Dashboard component configuration
	Dashboard dashboard.Dashboard `json:"dashboard,omitempty"`

	// Workbenches component configuration
	Workbenches workbenches.Workbenches `json:"workbenches,omitempty"`

	// ModelMeshServing component configuration
	ModelMeshServing modelmeshserving.ModelMeshServing `json:"modelmeshserving,omitempty"`

	// DataServicePipeline component configuration
	DataSciencePipelines datasciencepipelines.DataSciencePipelines `json:"datasciencepipelines,omitempty"`
}

// DataScienceClusterStatus defines the observed state of DataScienceCluster
type DataScienceClusterStatus struct {
	// Phase describes the Phase of DataScienceCluster reconciliation state
	// This is used by OLM UI to provide status information
	// to the user
	Phase string `json:"phase,omitempty"`

	// Conditions describes the state of the DataScienceCluster resource.
	// +optional
	Conditions []conditionsv1.Condition `json:"conditions,omitempty"`

	// RelatedObjects is a list of objects created and maintained by this
	// operator. Object references will be added to this list after they have
	// been created AND found in the cluster.
	// +optional
	RelatedObjects []corev1.ObjectReference `json:"relatedObjects,omitempty"`
	ErrorMessage   string                   `json:"errorMessage,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// DataScienceCluster is the Schema for the datascienceclusters API
type DataScienceCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DataScienceClusterSpec   `json:"spec,omitempty"`
	Status DataScienceClusterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// DataScienceClusterList contains a list of DataScienceCluster
type DataScienceClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DataScienceCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DataScienceCluster{}, &DataScienceClusterList{})
}
