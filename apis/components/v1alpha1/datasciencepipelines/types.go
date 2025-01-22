// +kubebuilder:object:generate=true

// Package datasciencepipelines provides a set of types used for DataSciencePipelines component
package datasciencepipelines

import operatorv1 "github.com/openshift/api/operator/v1"

type PreloadedPipelinesSpec struct {
	// Configures whether to automatically import the InstructLab pipeline.
	// You must enable the trainingoperator component to run the InstructLab pipeline.
	InstructLab PreloadedPipelineOptions `json:"instructLab,omitempty"`
}

type PreloadedPipelineOptions struct {
	// Set to one of the following values:
	//
	// - "Managed" : This pipeline is automatically imported.
	// - "Removed" : This pipeline is not automatically imported when a new pipeline server or DSPA is created. If previously set to "Managed", setting to "Removed" does not remove existing preloaded pipelines but does prevent future updates from being imported.
	//
	// +kubebuilder:validation:Enum=Managed;Removed
	// +kubebuilder:default=Removed
	State operatorv1.ManagementState `json:"state,omitempty"`
}
