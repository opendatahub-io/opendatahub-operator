// +kubebuilder:object:generate=true

// Package datasciencepipelines provides a set of types used for DataSciencePipelines component
package datasciencepipelines

import operatorv1 "github.com/openshift/api/operator/v1"

type PreloadedPipelinesSpec struct {
	// Configure whether to auto import the InstructLab pipeline on any new pipeline server (or DSPA) creation.
	// You must enable the trainingoperator component to run the InstructLab pipeline.
	InstructLab PreloadedPipelineOptions `json:"instructLab,omitempty"`
}

type PreloadedPipelineOptions struct {
	// Set to one of the following values:
	//
	// - "Managed" : Upon any new pipeline server (or DSPA) creation this pipeline is auto imported
	// - "Removed" : Upon any new pipeline server (or DSPA) creation this pipeline is not auto imported
	//
	// +kubebuilder:validation:Enum=Managed;Removed
	// +kubebuilder:default=Removed
	State operatorv1.ManagementState `json:"state,omitempty"`
}
