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

package v2

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DevFlags defines list of fields that can be used by developers to test customizations. This is not recommended
// to be used in production environment.
type DevFlags struct {
	// Override Zap log level. Can be "debug", "info", "error" or a number (more verbose).
	// +optional
	LogLevel string `json:"logLevel,omitempty"`
}

type TrustedCABundleSpec struct {
	// managementState indicates whether and how the operator should manage customized CA bundle
	// +kubebuilder:validation:Enum=Managed;Removed;Unmanaged
	// +kubebuilder:default=Removed
	ManagementState operatorv1.ManagementState `json:"managementState"`
	// A custom CA bundle that will be available for  all  components in the
	// Data Science Cluster(DSC). This bundle will be stored in odh-trusted-ca-bundle
	// ConfigMap .data.odh-ca-bundle.crt .
	// +kubebuilder:default=""
	CustomCABundle string `json:"customCABundle"`
}

// DSCInitializationStatus defines the observed state of DSCInitialization.
type DSCInitializationStatus struct {
	// Phase describes the Phase of DSCInitializationStatus
	// This is used by OLM UI to provide status information to the user
	Phase string `json:"phase,omitempty"`

	// Conditions describes the state of the DSCInitializationStatus resource
	// +optional
	Conditions []common.Condition `json:"conditions,omitempty"`

	// RelatedObjects is a list of objects created and maintained by this operator.
	// Object references will be added to this list after they have been created AND found in the cluster
	// +optional
	RelatedObjects []corev1.ObjectReference `json:"relatedObjects,omitempty"`
	ErrorMessage   string                   `json:"errorMessage,omitempty"`

	// Version and release type
	Release common.Release `json:"release,omitempty"`
}

// GetConditions returns the conditions slice
func (d *DSCInitializationStatus) GetConditions() []common.Condition {
	return d.Conditions
}

// SetConditions sets the conditions slice
func (d *DSCInitializationStatus) SetConditions(conditions []common.Condition) {
	d.Conditions = conditions
}

//+kubebuilder:object:root=true
//+kubebuilder:resource:scope=Cluster,shortName=dsci
//+kubebuilder:subresource:status
//+kubebuilder:storageversion
//+kubebuilder:printcolumn:name="Age",type=date,JSONPath=.metadata.creationTimestamp
//+kubebuilder:printcolumn:name="Phase",type=string,JSONPath=.status.phase,description="Current Phase"
//+kubebuilder:printcolumn:name="Created At",type=string,JSONPath=.metadata.creationTimestamp

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

func init() {
	SchemeBuilder.Register(
		&DSCInitialization{},
		&DSCInitializationList{},
	)
}
