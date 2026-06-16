package v1alpha1

import (
	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

const (
	AIGatewayComponentName = "aigateway"
	AIGatewayInstanceName  = "default-" + AIGatewayComponentName
	AIGatewayKind          = "AIGateway"
)

// AIGatewayCommonSpec defines the user-facing configuration for AIGateway,
// shared between DSC and the AIGateway CR.
type AIGatewayCommonSpec struct {
	// ModelsAsService controls the Models as a Service sub-component.
	ModelsAsService DSCModelsAsServiceSpec `json:"modelsasservice,omitempty"`
	// BatchGateway controls the batch-gateway operator sub-component.
	BatchGateway AIGatewayBatchGatewaySpec `json:"batchGateway,omitempty"`
}

// AIGatewayBatchGatewaySpec configures the batch-gateway operator lifecycle.
type AIGatewayBatchGatewaySpec struct {
	// +kubebuilder:validation:Enum=Managed;Removed
	ManagementState operatorv1.ManagementState `json:"managementState,omitempty"`
}

type AIGatewayCommonStatus struct{}

// DSCAIGateway contains all the configuration exposed in DSC instance for AIGateway component.
type DSCAIGateway struct {
	common.ManagementSpec `json:",inline"`
	AIGatewayCommonSpec   `json:",inline"`
}

// DSCAIGatewayStatus struct holds the status for the AIGateway component exposed in the DSC.
type DSCAIGatewayStatus struct {
	common.ManagementSpec  `json:",inline"`
	*AIGatewayCommonStatus `json:",inline"`
}
