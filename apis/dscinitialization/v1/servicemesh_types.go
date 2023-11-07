package v1

import operatorv1 "github.com/openshift/api/operator/v1"

// ServiceMeshSpec configures Service Mesh.
type ServiceMeshSpec struct {
	// +kubebuilder:validation:Enum=Managed;Removed
	// +kubebuilder:default=Removed
	ManagementState operatorv1.ManagementState `json:"managementState,omitempty"`
	// Mesh holds configuration of Service Mesh used by Opendatahub.
	Mesh ControlPlaneSpec `json:"controlPlane,omitempty"`
}

type ControlPlaneSpec struct {
	// Name is a name Service Mesh Control Plane. Defaults to "minimal".
	// +kubebuilder:default=data-science-smcp
	Name string `json:"name,omitempty"`
	// Namespace is a namespace where Service Mesh is deployed. Defaults to "istio-system".
	// +kubebuilder:default=istio-system
	Namespace string `json:"namespace,omitempty"`
	// MetricsCollection specifies if metrics from components on the Mesh namespace
	// should be collected. Setting the value to "Istio" will collect metrics from the
	// control plane and any proxies on the Mesh namespace (like gateway pods). Setting
	// to "None" will disable metrics collection.
	// +kubebuilder:validation:Enum=Istio;None
	// +kubebuilder:default=Istio
	MetricsCollection string `json:"monitoring,omitempty"`
	// IdentityType specifies the identity implementation used in the Mesh. For ROSA
	// clusters, you would need to set this to ThirdParty.
	// +kubebuilder:validation:Enum=Kubernetes;ThirdParty
	// +kubebuilder:default=Kubernetes
	IdentityType string `json:"identityType,omitempty"`
}
