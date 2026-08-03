/*
Copyright 2025.

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
	ModelsAsServiceComponentName = "modelsasservice"
	// value should match whats set in the XValidation below
	ModelsAsServiceInstanceName = "default-" + ModelsAsServiceComponentName
	ModelsAsServiceKind         = "ModelsAsService"
)

// DSCModelsAsServiceSpec enables ModelsAsService integration
type DSCModelsAsServiceSpec struct {
	// +kubebuilder:validation:Enum=Managed;Removed
	// +kubebuilder:default=Removed
	ManagementState operatorv1.ManagementState `json:"managementState,omitempty"`
}

// DSCModelsAsServiceStatus contains the observed state of the ModelsAsService exposed in the DSC instance
type DSCModelsAsServiceStatus struct {
	common.ManagementSpec `json:",inline"`
}
