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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// OdhDeploymentSpec defines the desired state of OdhDeployment
type OdhDeploymentSpec struct {
	Components []Component `json:"components,omitempty"`
	Version    string      `json:"version,omitempty"`
}

type Component struct {
	Name              string            `json:"name"`
	URL               string            `json:"url"`
	ManifestFolder    string            `json:"manifestFolder,omitempty"`
	CustomParameters  map[string]string `json:"customParameters,omitempty"`
	PostInstallScript string            `json:"preInstallScript,omitempty"`
	PreInstallScript  string            `json:"postInstallScript,omitempty"`
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

func init() {
	SchemeBuilder.Register(&OdhDeployment{}, &OdhDeploymentList{})
}
