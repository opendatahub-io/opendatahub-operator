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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ModelRegistryComponentName = "modelregistry"
	// ModelRegistryInstanceName the name of the ModelRegistry instance singleton.
	// value should match what's set in the XValidation below
	ModelRegistryInstanceName = "default-" + ModelRegistryComponentName
	ModelRegistryKind         = "ModelRegistry"
)

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = (*ModelRegistry)(nil)

// ModelRegistryCommonSpec spec defines the shared desired state of ModelRegistry
type ModelRegistryCommonSpec struct {
	// model registry spec exposed to DSC api
	common.DevFlagsSpec `json:",inline"`

	// Namespace for model registries to be installed, configurable only once when model registry is enabled, defaults to "rhoai-model-registries"
	// +kubebuilder:default="rhoai-model-registries"
	// +kubebuilder:validation:Pattern="^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$"
	// +kubebuilder:validation:MaxLength=63
	RegistriesNamespace string `json:"registriesNamespace,omitempty"`
}

// ModelRegistrySpec defines the desired state of ModelRegistry
type ModelRegistrySpec struct {
	// model registry spec exposed to DSC api
	ModelRegistryCommonSpec `json:",inline"`
	//  model registry spec exposed only to internal api
}

// ModelRegistryCommonStatus defines the shared observed state of ModelRegistry
type ModelRegistryCommonStatus struct {
	RegistriesNamespace           string `json:"registriesNamespace,omitempty"`
	common.ComponentReleaseStatus `json:",inline"`
}

// ModelRegistryStatus defines the observed state of ModelRegistry
type ModelRegistryStatus struct {
	common.Status             `json:",inline"`
	ModelRegistryCommonStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-modelregistry'",message="ModelRegistry name must be default-modelregistry"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// ModelRegistry is the Schema for the modelregistries API
type ModelRegistry struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModelRegistrySpec   `json:"spec,omitempty"`
	Status ModelRegistryStatus `json:"status,omitempty"`
}

func (c *ModelRegistry) GetDevFlags() *common.DevFlags {
	return c.Spec.DevFlags
}

func (c *ModelRegistry) GetStatus() *common.Status {
	return &c.Status.Status
}

func (c *ModelRegistry) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *ModelRegistry) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

func (c *ModelRegistry) GetReleaseStatus() *[]common.ComponentRelease {
	return &c.Status.Releases
}

func (c *ModelRegistry) SetReleaseStatus(releases []common.ComponentRelease) {
	c.Status.Releases = releases
}

// +kubebuilder:object:root=true

// ModelRegistryList contains a list of ModelRegistry
type ModelRegistryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ModelRegistry `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ModelRegistry{}, &ModelRegistryList{})
}

// +kubebuilder:object:generate=true
// +kubebuilder:validation:XValidation:rule="(self.managementState != 'Managed') || (oldSelf.registriesNamespace == '') || (oldSelf.managementState != 'Managed')|| (self.registriesNamespace == oldSelf.registriesNamespace)",message="RegistriesNamespace is immutable when model registry is Managed"
//nolint:lll

// DSCModelRegistry contains all the configuration exposed in DSC instance for ModelRegistry component
type DSCModelRegistry struct {
	// configuration fields common across components
	common.ManagementSpec `json:",inline"`
	// model registry specific field
	ModelRegistryCommonSpec `json:",inline"`
}

// DSCModelRegistryStatus struct holds the status for the ModelRegistry component exposed in the DSC
type DSCModelRegistryStatus struct {
	common.ManagementSpec      `json:",inline"`
	*ModelRegistryCommonStatus `json:",inline"`
}
