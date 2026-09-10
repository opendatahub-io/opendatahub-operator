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
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

const (
	TrainerComponentName = "trainer"
	// value should match whats set in the XValidation below
	TrainerInstanceName = "default-" + TrainerComponentName
	TrainerKind         = "Trainer"
)

type TrainerCommonSpec struct{}

// TrainerCommonStatus defines the shared observed state of Trainer
type TrainerCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// DSCTrainer contains all the configuration exposed in DSC instance for Trainer component
type DSCTrainer struct {
	common.ManagementSpec `json:",inline"`
	// configuration fields common across components
	TrainerCommonSpec `json:",inline"`
}

// DSCTrainerStatus struct holds the status for the Trainer component exposed in the DSC
type DSCTrainerStatus struct {
	common.ManagementSpec `json:",inline"`
	*TrainerCommonStatus  `json:",inline"`
}
