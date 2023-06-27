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
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// List of constants to show different  reconciliation messages and statuses.
const (
	ReconcileFailed           = "ReconcileFailed"
	ReconcileInit             = "ReconcileInit"
	ReconcileCompleted        = "ReconcileCompleted"
	ReconcileCompletedMessage = "Reconcile completed successfully"
)

// DSCInitializationSpec defines the desired state of DSCInitialization
type DSCInitializationSpec struct {
	Namespaces   []string   `json:"namespaces"`
	Monitoring   Monitoring `json:"monitoring,omitempty"`
	ManifestsUri string     `json:"manifestsUri,omitempty"`
}

type Monitoring struct {
	Enabled   bool   `json:"enabled,omitempty"`
	Namespace string `json:"namespace"`
}

// DSCInitializationStatus defines the observed state of DSCInitialization
type DSCInitializationStatus struct {
	// Phase describes the Phase of DSCInitializationStatus
	// This is used by OLM UI to provide status information
	// to the user
	Phase string `json:"phase,omitempty"`

	// Conditions describes the state of the DSCInitializationStatus resource.
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
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Age",type=date,JSONPath=.metadata.creationTimestamp
//+kubebuilder:printcolumn:name="Phase",type=string,JSONPath=.status.phase,description="Current Phase"
//+kubebuilder:printcolumn:name="Created At",type=string,JSONPath=.metadata.creationTimestamp
//+operator-sdk:csv:customresourcedefinitions:displayName="DSC Initialization"

// DSCInitialization is the Schema for the dscinitializations API
type DSCInitialization struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DSCInitializationSpec   `json:"spec,omitempty"`
	Status DSCInitializationStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// DSCInitializationList contains a list of DSCInitialization
type DSCInitializationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DSCInitialization `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DSCInitialization{}, &DSCInitializationList{})
}
