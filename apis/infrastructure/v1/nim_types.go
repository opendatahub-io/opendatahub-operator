package v1

import (
	operatorv1 "github.com/openshift/api/operator/v1"
)

// nimSpec enables NVIDIA NIM integration
type NimSpec struct {
	// +kubebuilder:validation:Enum=Managed;Removed
	// +kubebuilder:default=Managed
	ManagementState operatorv1.ManagementState `json:"managementState,omitempty"`
}
