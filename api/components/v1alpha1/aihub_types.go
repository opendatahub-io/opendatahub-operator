/*
Copyright 2026.

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

import "github.com/opendatahub-io/opendatahub-operator/v2/api/common"

// AIHubCommonStatus defines the observed state shared by the public v3 AI Hub
// component.
type AIHubCommonStatus struct {
	ApplicationNamespace          string `json:"applicationNamespace,omitempty"`
	common.ComponentReleaseStatus `json:",inline"`
}

// +kubebuilder:validation:XValidation:rule="(!has(self.managementState) || self.managementState != 'Managed') || (oldSelf.applicationNamespace == '') || (!has(oldSelf.managementState) || oldSelf.managementState != 'Managed') || (self.applicationNamespace == oldSelf.applicationNamespace)",message="ApplicationNamespace is immutable when AI Hub is Managed"
//nolint:lll

// DSCAIHub contains the public v3 AI Hub configuration exposed in the DSC.
type DSCAIHub struct {
	common.ManagementSpec `json:",inline"`
	AIHubCommonSpec       `json:",inline"`
}

// DSCAIHubStatus contains the public v3 AI Hub status exposed in the DSC.
type DSCAIHubStatus struct {
	common.ManagementSpec `json:",inline"`
	*AIHubCommonStatus    `json:",inline"`
}
