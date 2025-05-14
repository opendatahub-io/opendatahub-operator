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
	ServiceMeshServiceName = "servicemesh"
	// ServiceMeshInstanceName the name of the ServiceMesh instance singleton.
	// value should match whats set in the XValidation below
	ServiceMeshInstanceName = "default-servicemesh"
	ServiceMeshKind         = "ServiceMesh"
)

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = (*ServiceMesh)(nil)

type ServiceMeshSpec struct {
	// +kubebuilder:validation:Enum=Managed;Unmanaged;Removed
	// +kubebuilder:default=Removed
	ManagementState operatorv1.ManagementState `json:"managementState,omitempty"`
	// servicemesh spec exposed to DSCI api
	// ControlPlane holds configuration of Service Mesh used by Opendatahub.
	ControlPlane ServiceMeshControlPlaneSpec `json:"controlPlane,omitempty"`
	// Auth holds configuration of authentication and authorization services
	// used by Service Mesh in Opendatahub.
	Auth ServiceMeshAuthSpec `json:"auth,omitempty"`
}

type ServiceMeshControlPlaneSpec struct {
	// Name is a name Service Mesh Control Plane. Defaults to "data-science-smcp".
	// +kubebuilder:default=data-science-smcp
	Name string `json:"name,omitempty"`
	// Namespace is a namespace where Service Mesh is deployed. Defaults to "istio-system".
	// +kubebuilder:default=istio-system
	// +kubebuilder:validation:Pattern="^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$"
	// +kubebuilder:validation:MaxLength=63
	Namespace string `json:"namespace,omitempty"`
	// MetricsCollection specifies if metrics from components on the Mesh namespace
	// should be collected. Setting the value to "Istio" will collect metrics from the
	// control plane and any proxies on the Mesh namespace (like gateway pods). Setting
	// to "None" will disable metrics collection.
	// +kubebuilder:validation:Enum=Istio;None
	// +kubebuilder:default=Istio
	MetricsCollection string `json:"metricsCollection,omitempty"`
}

type ServiceMeshAuthSpec struct {
	// Namespace where it is deployed. If not provided, the default is to
	// use '-auth-provider' suffix on the ApplicationsNamespace of the DSCI.
	// +kubebuilder:validation:Pattern="^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$"
	// +kubebuilder:validation:MaxLength=63
	Namespace string `json:"namespace,omitempty"`
	// Audiences is a list of the identifiers that the resource server presented
	// with the token identifies as. Audience-aware token authenticators will verify
	// that the token was intended for at least one of the audiences in this list.
	// If no audiences are provided, the audience will default to the audience of the
	// Kubernetes apiserver (kubernetes.default.svc).
	// +kubebuilder:default={"https://kubernetes.default.svc"}
	Audiences []string `json:"audiences,omitempty"`
}

// ServiceMeshStatus defines the observed state of ServiceMesh
type ServiceMeshStatus struct {
	common.Status `json:",inline"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-servicemesh'",message="ServiceMesh name must be default-servicemesh"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// ServiceMesh is the Schema for the servicemesh API
type ServiceMesh struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceMeshSpec   `json:"spec,omitempty"`
	Status ServiceMeshStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ServiceMeshList contains a list of ServiceMesh
type ServiceMeshList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ServiceMesh `json:"items"`
}

func (m *ServiceMesh) GetStatus() *common.Status {
	return &m.Status.Status
}

func (c *ServiceMesh) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *ServiceMesh) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

func init() {
	SchemeBuilder.Register(&ServiceMesh{}, &ServiceMeshList{})
}
