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
)

const (
	ModelRegistryComponentName = "modelregistry"
	// ModelRegistryInstanceName the name of the ModelRegistry instance singleton.
	// value should match what's set in the XValidation below
	ModelRegistryInstanceName = "default-" + ModelRegistryComponentName
	ModelRegistryKind         = "ModelRegistry"
)

// ModelRegistryCommonStatus defines the shared observed state of ModelRegistry
type ModelRegistryCommonStatus struct {
	RegistriesNamespace           string `json:"registriesNamespace,omitempty"`
	common.ComponentReleaseStatus `json:",inline"`
}

// +kubebuilder:validation:XValidation:rule="(!has(self.managementState) || self.managementState != 'Managed') || (oldSelf.registriesNamespace == '') || (!has(oldSelf.managementState) || oldSelf.managementState != 'Managed') || (self.registriesNamespace == oldSelf.registriesNamespace)",message="RegistriesNamespace is immutable when model registry is Managed"
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
