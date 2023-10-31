package v1

import (
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ServiceMeshSpec configures Service Mesh.
type ServiceMeshSpec struct {
	// +kubebuilder:validation:Enum=Managed;Removed
	// +kubebuilder:default=Removed
	ManagementState operatorv1.ManagementState `json:"managementState,omitempty"`
	// Mesh holds configuration of Service Mesh used by Opendatahub.
	Mesh MeshSpec `json:"mesh,omitempty"`
	// Auth holds configuration of authentication and authorization services
	// used by Service Mesh in Opendatahub.
	Auth AuthSpec `json:"auth,omitempty"`
}

type MeshSpec struct {
	// Name is a name Service Mesh Control Plan. Defaults to "basic".
	// +kubebuilder:default=basic
	Name string `json:"name,omitempty"`
	// Namespace is a namespace where Service Mesh is deployed. Defaults to "istio-system".
	// +kubebuilder:default=istio-system
	Namespace string `json:"namespace,omitempty"`
	// Certificate allows to define how to use certificates for the Service Mesh communication.
	Certificate CertSpec `json:"certificate,omitempty"`
}

type CertSpec struct {
	// Name of the certificate to be used by Service Mesh.
	// +kubebuilder:default=opendatahub-dashboard-cert
	Name string `json:"name,omitempty"`
	// Generate indicates if the certificate should be generated. If set to false
	// it will assume certificate with the given name is made available as a secret
	// in Service Mesh namespace.
	// +kubebuilder:default=true
	Generate bool `json:"generate,omitempty"`
}

type AuthSpec struct {
	// Name of the authorization provider used for Service Mesh.
	// +kubebuilder:default=authorino
	Name string `json:"name,omitempty"`
	// Namespace where it is deployed.
	// +kubebuilder:default=auth-provider
	Namespace string `json:"namespace,omitempty"`
	// Authorino holds configuration of Authorino service used as external authorization provider.
	Authorino AuthorinoSpec `json:"authorino,omitempty"`
}

type AuthorinoSpec struct {
	// Name specifies how external authorization provider should be called.
	// +kubebuilder:default=authorino-mesh-authz-provider
	Name string `json:"name,omitempty"`
	// Audiences is a list of the identifiers that the resource server presented
	// with the token identifies as. Audience-aware token authenticators will verify
	// that the token was intended for at least one of the audiences in this list.
	// If no audiences are provided, the audience will default to the audience of the
	// Kubernetes apiserver (kubernetes.default.svc).
	// +kubebuilder:default={"https://kubernetes.default.svc"}
	Audiences []string `json:"audiences,omitempty"`
	// Label narrows amount of AuthConfigs to process by Authorino service.
	// +kubebuilder:default=authorino/topic=odh
	Label string `json:"label,omitempty"`
	// Image allows to define a custom container image to be used when deploying Authorino's instance.
	// +kubebuilder:default="quay.io/kuadrant/authorino:v0.13.0"
	Image string `json:"image,omitempty"`
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

// TODO move logic to sth like management state
// IsValid returns true if the spec is a valid and complete.
// If invalid it provides message with the reasons.
func (s *ServiceMeshSpec) IsValid() (bool, string) {
	if s.Auth.Name != "authorino" {
		return false, "currently only Authorino is available as authorization layer"
	}

	return true, ""
}
