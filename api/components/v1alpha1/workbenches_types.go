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
	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

const (
	WorkbenchesComponentName = "workbenches"
	// WorkbenchesInstanceName the name of the Workbenches instance singleton.
	// value should match what is set in the XValidation below.
	WorkbenchesInstanceName = "default-" + WorkbenchesComponentName
	WorkbenchesKind         = "Workbenches"
)

// WorkbenchesCommonStatus defines the shared observed state of Workbenches.
// On the module CR, status.applicationsNamespace is the operand deploy target and
// status.workbenchNamespace echoes the legacy DSC notebooks namespace.
type WorkbenchesCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
	WorkbenchNamespace            string `json:"workbenchNamespace,omitempty"`
}

// WorkbenchesV2Spec configures the workbenches-v2 sub-component lifecycle.
type WorkbenchesV2Spec struct {
	// +kubebuilder:validation:Enum=Managed;Removed
	// +kubebuilder:default=Removed
	ManagementState operatorv1.ManagementState `json:"managementState,omitempty"`
}

// DSCWorkbenchesV2Status contains the observed state of the WorkbenchesV2 submodule.
type DSCWorkbenchesV2Status struct {
	common.ManagementSpec `json:",inline"`
}

// DSCWorkbenches contains all the configuration exposed in DSC instance for Workbenches component
type DSCWorkbenches struct {
	// configuration fields common across components
	common.ManagementSpec `json:",inline"`
	// workbenches specific field
	WorkbenchesCommonSpec `json:",inline"`
}

// DSCWorkbenchesStatus struct holds the status for the Workbenches component exposed in the DSC
type DSCWorkbenchesStatus struct {
	common.ManagementSpec    `json:",inline"`
	*WorkbenchesCommonStatus `json:",inline"`
}
