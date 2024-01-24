package v1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// FeatureTracker represents a cluster-scoped resource in the Data Science Cluster,
// specifically designed for monitoring and managing objects created via the internal Features API.
// This resource serves a crucial role in cross-namespace resource management, acting as
// an owner reference for various resources. The primary purpose of the FeatureTracker
// is to enable efficient garbage collection by Kubernetes. This is essential for
// ensuring that resources are automatically cleaned up and reclaimed when they are
// no longer required.
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
type FeatureTracker struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              FeatureTrackerSpec   `json:"spec,omitempty"`
	Status            FeatureTrackerStatus `json:"status,omitempty"`
}

type OwnerType string

const (
	ComponentType OwnerType = "Component"
	DSCIType      OwnerType = "DSCI"
)

func (s *FeatureTracker) ToOwnerReference() metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: s.APIVersion,
		Kind:       s.Kind,
		Name:       s.Name,
		UID:        s.UID,
	}
}

// Source describes the type of object that created the related Feature to this FeatureTracker.
type Source struct {
	Type OwnerType `json:"type,omitempty"`
	Name string    `json:"name,omitempty"`
}

// FeatureTrackerSpec defines the desired state of FeatureTracker.
type FeatureTrackerSpec struct {
	Source       Source `json:"source,omitempty"`
	AppNamespace string `json:"appNamespace,omitempty"`
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
		&FeatureTracker{},
		&FeatureTrackerList{},
	)
}
