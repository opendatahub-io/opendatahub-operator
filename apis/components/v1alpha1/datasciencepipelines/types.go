// +kubebuilder:object:generate=true

// Package datasciencepipelines provides a set of types used for DataSciencePipelines component
package datasciencepipelines

import operatorv1 "github.com/openshift/api/operator/v1"

type PreloadedPipelinesSpec struct {
	InstructLab InstructLabPipelineSpec `json:"instructLab,omitempty"`
}

type InstructLabPipelineSpec struct {
	// Set to one of the following values:
	//
	// - "Managed" : TODO
	// - "Removed" : TODO
	//
	// +kubebuilder:validation:Enum=Managed;Removed
	// +kubebuilder:default=Removed
	State operatorv1.ManagementState `json:"state,omitempty"`
}
