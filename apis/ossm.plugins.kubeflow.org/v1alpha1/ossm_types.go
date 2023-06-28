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

// OssmPluginSpec defines the extra data provided by the Openshift Service Mesh Plugin in KfDef spec.
type OssmPluginSpec struct {
	Mesh MeshSpec `json:"mesh,omitempty"`
	Auth AuthSpec `json:"auth,omitempty"`
}

type MeshSpec struct {
	Name        string   `json:"name,omitempty"`
	Namespace   string   `json:"namespace,omitempty"`
	Certificate CertSpec `json:"certificate,omitempty"`
}

type CertSpec struct {
	Name     string `json:"name,omitempty" default:"opendatahub-self-signed-cert"`
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

// OssmPluginList contains a list of GcpPlugin
type OssmPluginList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OssmPlugin `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OssmPlugin{}, &OssmPluginList{})
}
