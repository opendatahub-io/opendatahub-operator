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
	MCPLifecycleOperatorComponentName = "mcplifecycleoperator"
	MCPLifecycleOperatorInstanceName  = "default-" + MCPLifecycleOperatorComponentName
	MCPLifecycleOperatorKind          = "MCPLifecycleOperator"
)

// MCPLifecycleOperatorCommonSpec holds config fields shared between the
// standalone CRD (owned by the module operator) and the DSC embedding.
type MCPLifecycleOperatorCommonSpec struct{}

// MCPLifecycleOperatorCommonStatus defines the shared observed state of MCPLifecycleOperator.
type MCPLifecycleOperatorCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// DSCMCPLifecycleOperator contains all the configuration exposed in DSC instance for MCPLifecycleOperator component.
type DSCMCPLifecycleOperator struct {
	common.ManagementSpec          `json:",inline"`
	MCPLifecycleOperatorCommonSpec `json:",inline"`
}

// DSCMCPLifecycleOperatorStatus struct holds the status for the MCPLifecycleOperator component exposed in the DSC.
type DSCMCPLifecycleOperatorStatus struct {
	common.ManagementSpec             `json:",inline"`
	*MCPLifecycleOperatorCommonStatus `json:",inline"`
}
