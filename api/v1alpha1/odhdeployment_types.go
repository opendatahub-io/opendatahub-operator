/*
Copyright 2022.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// OdhDeploymentSpec defines the desired state of OdhDeployment
type OdhDeploymentSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// List of AI/ML components that need to be installed
	Components []Component `json:"components,omitempty"`
	// Manifest version
	// A user can update the version value to update the components
	Version string `json:"version"`
	// List of repositories for the component manifests
	Repos []Repo `json:"repos,omitempty"`
}

// OdhDeploymentStatus defines the observed state of OdhDeployment
type OdhDeploymentStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// OdhDeployment is the Schema for the odhdeployments API
type OdhDeployment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OdhDeploymentSpec   `json:"spec,omitempty"`
	Status OdhDeploymentStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// OdhDeploymentList contains a list of OdhDeployment
type OdhDeploymentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OdhDeployment `json:"items"`
}

// Component defines user options for a Component
type Component struct {
	// name of the component
	Name string `json:"name"`
	// manifests defines path of the component in a repo
	Manifests Manifests `json:"manifests"`
	// enabled field if set to True installs the component and when set to False, uninstalls it.
	Enabled bool `json:"enabled"`
	// reconcile if set to False, will allow user to make any changes to the Resources
	// Additionally when reconcile field is set to False, an update in version won't update the manifests for the given component.
	Reconcile bool `json:"reconcile"`
}

// Repo defines the manifest repository for all the components
type Repo struct {
	// name of the repository
	Name string `json:"name"`
	// uri is the Github url or path to local folder for the manifests
	Uri string `json:"uri"`
}

type Manifests struct {
	// RepoName name is the name defined in the repo struct
	RepoName string `json:"repoName"`
	// relative path of the component in the repo
	Path string `json:"path"`
}

func init() {
	SchemeBuilder.Register(&OdhDeployment{}, &OdhDeploymentList{})
}
