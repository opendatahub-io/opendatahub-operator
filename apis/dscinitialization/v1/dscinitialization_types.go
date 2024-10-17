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

	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

// +operator-sdk:csv:customresourcedefinitions:order=1

// DSCInitializationSpec defines the desired state of DSCInitialization.
type DSCInitializationSpec struct {
	// +kubebuilder:default:=redhat-ods-applications
	// Namespace for applications to be installed, non-configurable, default to "redhat-ods-applications"
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="ApplicationsNamespace is immutable"
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=1
	ApplicationsNamespace string `json:"applicationsNamespace"`
	// Enable monitoring on specified namespace
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=2
	// +optional
	Monitoring Monitoring `json:"monitoring,omitempty"`
	// Configures Service Mesh as networking layer for Data Science Clusters components.
	// The Service Mesh is a mandatory prerequisite for single model serving (KServe) and
	// you should review this configuration if you are planning to use KServe.
	// For other components, it enhances user experience; e.g. it provides unified
	// authentication giving a Single Sign On experience.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=3
	// +optional
	ServiceMesh *infrav1.ServiceMeshSpec `json:"serviceMesh,omitempty"`
	// When set to `Managed`, adds odh-trusted-ca-bundle Configmap to all namespaces that includes
	// cluster-wide Trusted CA Bundle in .data["ca-bundle.crt"].
	// Additionally, this fields allows admins to add custom CA bundles to the configmap using the .CustomCABundle field.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=4
	// +optional
	TrustedCABundle *TrustedCABundleSpec `json:"trustedCABundle,omitempty"`
	// Internal development useful field to test customizations.
	// This is not recommended to be used in production environment.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=5
	// +optional
	DevFlags *DevFlags `json:"devFlags,omitempty"`
}

type Monitoring struct {
	// Set to one of the following values:
	// - "Managed" : the operator is actively managing the component and trying to keep it active.
	//               It will only upgrade the component if it is safe to do so.
	// - "Removed" : the operator is actively managing the component and will not install it,
	//               or if it is installed, the operator will try to remove it.
	// +kubebuilder:validation:Enum=Managed;Removed
	ManagementState operatorv1.ManagementState `json:"managementState,omitempty"`
	// +kubebuilder:default=redhat-ods-monitoring
	// Namespace for monitoring if it is enabled
	Namespace string `json:"namespace,omitempty"`
}

// DevFlags defines list of fields that can be used by developers to test customizations. This is not recommended
// to be used in production environment.
type DevFlags struct {
	// Custom manifests uri for odh-manifests
	// +optional
	ManifestsUri string `json:"manifestsUri,omitempty"`
	// +kubebuilder:validation:Enum=devel;development;prod;production
	// +kubebuilder:default="production"
	LogMode string `json:"logmode,omitempty"`
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
	// +operator-sdk:csv:customresourcedefinitions:type=status
	// +optional
	Conditions []conditionsv1.Condition `json:"conditions,omitempty"`

	// RelatedObjects is a list of objects created and maintained by this operator.
	// Object references will be added to this list after they have been created AND found in the cluster
	// +optional
	RelatedObjects []corev1.ObjectReference `json:"relatedObjects,omitempty"`
	ErrorMessage   string                   `json:"errorMessage,omitempty"`

	// Version and release type
	Release cluster.Release `json:"release,omitempty"`
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

func init() {
	SchemeBuilder.Register(
		&DSCInitialization{},
		&DSCInitializationList{},
	)
}
