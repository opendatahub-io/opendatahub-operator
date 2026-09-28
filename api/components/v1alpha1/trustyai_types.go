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
	TrustyAIComponentName = "trustyai"
	// value should match whats set in the XValidation below
	TrustyAIInstanceName = "default-" + TrustyAIComponentName
	TrustyAIKind         = "TrustyAI"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// TrustyAIEvalSpec defines evaluation configuration for TrustyAI
type TrustyAIEvalSpec struct {
	// LMEval configuration for model evaluations
	LMEval TrustyAILMEvalSpec `json:"lmeval"`
}

// TrustyAILMEvalSpec defines configuration for LMEval evaluations
type TrustyAILMEvalSpec struct {
	// PermitCodeExecution controls whether code execution is allowed during evaluations
	// +kubebuilder:default="deny"
	// +kubebuilder:validation:Enum=allow;deny
	PermitCodeExecution string `json:"permitCodeExecution,omitempty"`
	// PermitOnline controls whether online access is allowed during evaluations
	// +kubebuilder:default="deny"
	// +kubebuilder:validation:Enum=allow;deny
	PermitOnline string `json:"permitOnline,omitempty"`
}

type TrustyAICommonSpec struct {
	// Eval configuration for TrustyAI evaluations
	Eval TrustyAIEvalSpec `json:"eval,omitempty"`
	// MCPGuardrailsMode enables the mcp-guardrails overlay when set to true
	// +kubebuilder:default=false
	MCPGuardrailsMode bool `json:"mcpGuardrailsMode,omitempty"`
}

// TrustyAICommonStatus defines the shared observed state of TrustyAI
type TrustyAICommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// DSCTrustyAI contains all the configuration exposed in DSC instance for TrustyAI component
type DSCTrustyAI struct {
	common.ManagementSpec `json:",inline"`
	// configuration fields common across components
	TrustyAICommonSpec `json:",inline"`
}

// DSCTrustyAIStatus struct holds the status for the TrustyAI component exposed in the DSC
type DSCTrustyAIStatus struct {
	common.ManagementSpec `json:",inline"`
	*TrustyAICommonStatus `json:",inline"`
}
