package v1alpha1

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

const (
	AgentsOperatorComponentName = "agentsoperator"
	// value should match what is set in the XValidation below
	AgentsOperatorInstanceName = "default-agentsoperator"
	AgentsOperatorKind         = "AgentsOperator"
)

// AgentsOperatorCommonSpec defines configuration shared between DSC and module CR.
type AgentsOperatorCommonSpec struct {
	// Auth is projected by the platform operator when authentication is enabled.
	// +optional
	Auth *AgentsOperatorAuth `json:"auth,omitempty"`
}

// AgentsOperatorAuth holds platform-projected authentication settings.
type AgentsOperatorAuth struct {
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=256
	// +optional
	Audiences []string `json:"audiences,omitempty"`
}

// DSCAgentsOperator defines the configuration exposed in the DSC for Agents Operator.
type DSCAgentsOperator struct {
	common.ManagementSpec    `json:",inline"`
	AgentsOperatorCommonSpec `json:",inline"`
}

// DSCAgentsOperatorStatus holds the observed state of Agents Operator exposed in the DSC.
type DSCAgentsOperatorStatus struct {
	common.ManagementSpec `json:",inline"`
}
