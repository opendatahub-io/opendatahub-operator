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
	ModelMeshServingComponentName = "modelmeshserving"
	// value should match whats set in the XValidation below
	ModelMeshServingInstanceName = "default-" + ModelMeshServingComponentName
	ModelMeshServingKind         = "ModelMeshServing"
)

// NOTE: This file contains MINIMAL types required for v1 DSC API compatibility only.

// ModelMeshServingCommonStatus defines the common status for ModelMeshServing (minimal for v1 compatibility)
type ModelMeshServingCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// DSCModelMeshServing contains all the configuration exposed in DSC instance for ModelMeshServing component
type DSCModelMeshServing struct {
	common.ManagementSpec `json:",inline"`
}

// DSCModelMeshServingStatus contains the observed state of the ModelMeshServing exposed in the DSC instance
type DSCModelMeshServingStatus struct {
	common.ManagementSpec         `json:",inline"`
	*ModelMeshServingCommonStatus `json:",inline"`
}
