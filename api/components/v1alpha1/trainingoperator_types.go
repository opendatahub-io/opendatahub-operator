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
	TrainingOperatorComponentName = "trainingoperator"
	TrainingOperatorKind          = "TrainingOperator"
)

type TrainingOperatorCommonSpec struct{}

// TrainingOperatorCommonStatus defines the shared observed state of TrainingOperator
type TrainingOperatorCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// +kubebuilder:validation:XValidation:rule="!has(self.managementState) || self.managementState != 'Managed' || (has(oldSelf.managementState) && oldSelf.managementState == 'Managed')",message="TrainingOperator v1 is obsolete in RHOAI 3.6. Set managementState to Removed, then delete the TrainingOperator CR to clean up. Use Trainer v2 instead."
//nolint:lll

// DSCTrainingOperator contains all the configuration exposed in DSC instance for TrainingOperator component.
// The XValidation rule lives here (not on the v2-only Components struct) so it applies to v1 too.
type DSCTrainingOperator struct {
	common.ManagementSpec `json:",inline"`
	// configuration fields common across components
	TrainingOperatorCommonSpec `json:",inline"`
}

// DSCTrainingOperatorStatus struct holds the status for the TrainingOperator component exposed in the DSC
type DSCTrainingOperatorStatus struct {
	common.ManagementSpec         `json:",inline"`
	*TrainingOperatorCommonStatus `json:",inline"`
}
