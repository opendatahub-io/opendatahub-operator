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
	FeastOperatorComponentName = "feastoperator"
	// FeastOperatorInstanceName is the singleton name for the FeastOperator instance.
	FeastOperatorInstanceName = "default-" + FeastOperatorComponentName
	FeastOperatorKind         = "FeastOperator"
)

// FeastOperatorCommonSpec defines the common spec shared across APIs for FeastOperator
type FeastOperatorCommonSpec struct {
	// Spec fields exposed to the DSC API
}

// FeastOperatorCommonStatus defines the shared observed state of FeastOperator
type FeastOperatorCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// DSCFeastOperator defines the configuration exposed in the DSC instance for FeastOperator
type DSCFeastOperator struct {
	// Fields common across components
	common.ManagementSpec `json:",inline"`

	// FeastOperator-specific fields
	FeastOperatorCommonSpec `json:",inline"`
}

// DSCFeastOperatorStatus struct holds the status for the FeastOperator component exposed in the DSC
type DSCFeastOperatorStatus struct {
	common.ManagementSpec      `json:",inline"`
	*FeastOperatorCommonStatus `json:",inline"`
}
