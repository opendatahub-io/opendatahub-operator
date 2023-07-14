package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type OssmPlugin struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OssmPluginSpec   `json:"spec,omitempty"`
	Status OssmPluginStatus `json:"status,omitempty"`
}

// OssmPluginSpec defines configuration needed for Openshift Service Mesh
// for integration with Opendatahub.
type OssmPluginSpec struct {
	Mesh MeshSpec `json:"mesh,omitempty"`
	Auth AuthSpec `json:"auth,omitempty"`
}

// MeshSpec holds information on how Service Mesh should be configured.
type MeshSpec struct {
	Name        string   `json:"name,omitempty"`
	Namespace   string   `json:"namespace,omitempty"`
	Certificate CertSpec `json:"certificate,omitempty"`
}

type CertSpec struct {
	Name     string `json:"name,omitempty"`
	Generate bool   `json:"generate,omitempty"`
}

type AuthSpec struct {
	Name      string        `json:"name,omitempty"`
	Namespace string        `json:"namespace,omitempty"`
	Authorino AuthorinoSpec `json:"authorino,omitempty"`
}

type AuthorinoSpec struct {
	Name  string `json:"name,omitempty"`
	Label string `json:"label,omitempty"`
	Image string `json:"image,omitempty"`
}

// OssmPluginStatus defines the observed state of OssmPlugin
type OssmPluginStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// TODO model conditions
}

//+kubebuilder:object:root=true

// OssmPluginList contains a list of OssmPlugins
type OssmPluginList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OssmPlugin `json:"items"`
}

// OssmResourceTracker is a cluster-scoped resource for tracking objects
// created by Ossm plugin. It's primarily used as owner reference
// for resources created across namespaces so that they can be
// garbage collected by Kubernetes when they're not needed anymore.
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
type OssmResourceTracker struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OssmResourceTrackerSpec   `json:"spec,omitempty"`
	Status OssmResourceTrackerStatus `json:"status,omitempty"`
}

// OssmResourceTrackerSpec defines the desired state of OssmResourceTracker
type OssmResourceTrackerSpec struct {
}

// OssmResourceTrackerStatus defines the observed state of OssmResourceTracker
type OssmResourceTrackerStatus struct {
}

// +kubebuilder:object:root=true

// OssmResourceTrackerList contains a list of OssmResourceTracker
type OssmResourceTrackerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OssmResourceTracker `json:"items"`
}

func init() {
	SchemeBuilder.Register(
		&OssmPlugin{},
		&OssmPluginList{},
		&OssmResourceTracker{},
		&OssmResourceTrackerList{},
	)
}
