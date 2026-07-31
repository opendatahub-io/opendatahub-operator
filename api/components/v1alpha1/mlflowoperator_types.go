package v1alpha1

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

const (
	MLflowOperatorComponentName = "mlflowoperator"
	// value should match whats set in the XValidation below
	MLflowOperatorInstanceName = "default-" + "mlflowoperator"
	MLflowOperatorKind         = "MLflowOperator"
)

type MLflowOperatorCommonSpec struct {
	// Gateway configuration for MLflow ingress (synced from GatewayConfig by the DSC controller
	// when creating the MLflowOperator CR).
	// +optional
	Gateway *common.GatewaySpec `json:"gateway,omitempty"`
	// GatewayName is the gateway resource name projected into the MLflowOperator singleton CR.
	// +optional
	GatewayName string `json:"gatewayName,omitempty"`
	// SectionTitle is the console section title projected into the MLflowOperator singleton CR.
	// +optional
	SectionTitle string `json:"sectionTitle,omitempty"`
}

// MLflowOperatorCommonStatus defines the shared observed state of MLflowOperator
type MLflowOperatorCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

type DSCMLflowOperator struct {
	common.ManagementSpec    `json:",inline"`
	MLflowOperatorCommonSpec `json:",inline"`
}

// DSCMLflowOperatorStatus contains the observed state of the MLflowOperator exposed in the DSC instance
type DSCMLflowOperatorStatus struct {
	common.ManagementSpec       `json:",inline"`
	*MLflowOperatorCommonStatus `json:",inline"`
}
