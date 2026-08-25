package v1alpha1

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

const (
	// Component name
	SparkOperatorComponentName = "sparkoperator"

	// SparkOperatorInstanceName is the name of the component instance singleton
	// value should match what is set in the kubebuilder markers for XValidation defined below
	SparkOperatorInstanceName = "default-sparkoperator"

	// Kubernetes kind of the component
	SparkOperatorKind = "SparkOperator"
)

type SparkOperatorCommonSpec struct {
	// TODO: Add Spark Operator specific configuration fields
}

// SparkOperatorCommonStatus defines the shared observed state
type SparkOperatorCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// DSCSparkOperator contains all the configuration exposed in DSC instance
type DSCSparkOperator struct {
	common.ManagementSpec   `json:",inline"`
	SparkOperatorCommonSpec `json:",inline"`
}

// DSCSparkOperatorStatus contains the observed state exposed in the DSC
type DSCSparkOperatorStatus struct {
	common.ManagementSpec      `json:",inline"`
	*SparkOperatorCommonStatus `json:",inline"`
}
