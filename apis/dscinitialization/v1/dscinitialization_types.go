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
	operatorv1 "github.com/openshift/api/operator/v1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +operator-sdk:csv:customresourcedefinitions:order=1

// DSCInitializationSpec defines the desired state of DSCInitialization.
type DSCInitializationSpec struct {
	// Namespace for applications to be installed, non-configurable, default to "opendatahub"
	// +kubebuilder:default:=opendatahub
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=1
	ApplicationsNamespace string `json:"applicationsNamespace"`
	// Enable monitoring on specified namespace
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=2
	// +optional
	Monitoring Monitoring `json:"monitoring,omitempty"`
	// Internal development useful field to test customizations.
	// This is not recommended to be used in production environment.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=3
	// +optional
	DevFlags DevFlags `json:"devFlags,omitempty"`
}

type Monitoring struct {
	// Set to one of the following values:
	// - "Managed" : the operator is actively managing the component and trying to keep it active.
	//               It will only upgrade the component if it is safe to do so.
	// - "Removed" : the operator is actively managing the component and will not install it,
	//               or if it is installed, the operator will try to remove it.
	// +kubebuilder:validation:Enum=Managed;Removed
	ManagementState operatorv1.ManagementState `json:"managementState,omitempty"`
	// +kubebuilder:default=opendatahub
	// Namespace for monitoring if it is enabled
	Namespace string `json:"namespace,omitempty"`
}

// DevFlags defines list of fields that can be used by developers to test customizations. This is not recommended
// to be used in production environment.
type DevFlags struct {
	// Custom manifests uri for odh-manifests
	// +optional
	ManifestsUri string `json:"manifestsUri,omitempty"` //nolint
}

// DSCInitializationStatus defines the observed state of DSCInitialization.
type DSCInitializationStatus struct {
	// Phase describes the Phase of DSCInitializationStatus
	// This is used by OLM UI to provide status information to the user
	Phase string `json:"phase,omitempty"`

	// Conditions describes the state of the DSCInitializationStatus resource
	// +operator-sdk:csv:customresourcedefinitions:type=status
	// +optional
	Conditions []conditionsv1.Condition `json:"conditions,omitempty"`

	// RelatedObjects is a list of objects created and maintained by this operator.
	// Object references will be added to this list after they have been created AND found in the cluster
	// +optional
	RelatedObjects []corev1.ObjectReference `json:"relatedObjects,omitempty"`
	ErrorMessage   string                   `json:"errorMessage,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:resource:scope=Cluster,shortName=dsci
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Age",type=date,JSONPath=.metadata.creationTimestamp
//+kubebuilder:printcolumn:name="Phase",type=string,JSONPath=.status.phase,description="Current Phase"
//+kubebuilder:printcolumn:name="Created At",type=string,JSONPath=.metadata.creationTimestamp
//+operator-sdk:csv:customresourcedefinitions:displayName="DSC Initialization"

// DSCInitialization is the Schema for the dscinitializations API.
type DSCInitialization struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DSCInitializationSpec   `json:"spec,omitempty"`
	Status DSCInitializationStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// DSCInitializationList contains a list of DSCInitialization.
type DSCInitializationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DSCInitialization `json:"items"`
}

// FeatureTracker is a cluster-scoped resource for tracking objects
// created through Features API for Data Science Platform.
// It's primarily used as owner reference for resources created across namespaces so that they can be
// garbage collected by Kubernetes when they're not needed anymore.
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
type FeatureTracker struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   FeatureTrackerSpec   `json:"spec,omitempty"`
	Status FeatureTrackerStatus `json:"status,omitempty"`
}

func (s *FeatureTracker) ToOwnerReference() metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: s.APIVersion,
		Kind:       s.Kind,
		Name:       s.Name,
		UID:        s.UID,
	}
}

// FeatureTrackerSpec defines the desired state of FeatureTracker.
type FeatureTrackerSpec struct {
}

// FeatureTrackerStatus defines the observed state of FeatureTracker.
type FeatureTrackerStatus struct {
}

// +kubebuilder:object:root=true

// FeatureTrackerList contains a list of FeatureTracker.
type FeatureTrackerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FeatureTracker `json:"items"`
}

func init() {
	SchemeBuilder.Register(
		&DSCInitialization{},
		&DSCInitializationList{},
		&FeatureTracker{},
		&FeatureTrackerList{},
	)
}
