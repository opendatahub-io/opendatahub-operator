package v1alpha1

import (
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

const (
	AIGatewayComponentName = "aigateway"
	AIGatewayInstanceName  = "default-aigateway"
	AIGatewayKind          = "AIGateway"
)

var _ common.PlatformObject = (*AIGateway)(nil)

// InferencePayloadProcessingSpec enables Inference Payload Processing (IPP) for AI Gateway.
// IPP runs as ext_proc filters in the gateway, handling model routing, API translation,
// and API key injection for inference requests.
type InferencePayloadProcessingSpec struct {
	// +kubebuilder:validation:Enum=Managed;Removed
	// +kubebuilder:default=Removed
	ManagementState operatorv1.ManagementState `json:"managementState,omitempty"`
}

// AIGatewayCommonSpec defines the shared desired state of AIGateway.
type AIGatewayCommonSpec struct {
	// Configures and enables Inference Payload Processing (IPP)
	InferencePayloadProcessing InferencePayloadProcessingSpec `json:"inferencePayloadProcessing,omitempty"`
}

// AIGatewaySpec defines the desired state of AIGateway.
type AIGatewaySpec struct {
	AIGatewayCommonSpec `json:",inline"`
}

// AIGatewayCommonStatus defines the shared observed state of AIGateway.
type AIGatewayCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// AIGatewayStatus defines the observed state of AIGateway.
type AIGatewayStatus struct {
	common.Status         `json:",inline"`
	AIGatewayCommonStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-aigateway'",message="AIGateway name must be default-aigateway"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// AIGateway is the Schema for the aigateways API
type AIGateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AIGatewaySpec   `json:"spec,omitempty"`
	Status AIGatewayStatus `json:"status,omitempty"`
}

func (c *AIGateway) GetStatus() *common.Status {
	return &c.Status.Status
}

func (c *AIGateway) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *AIGateway) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

func (c *AIGateway) GetReleaseStatus() *[]common.ComponentRelease {
	return &c.Status.Releases
}

func (c *AIGateway) SetReleaseStatus(releases []common.ComponentRelease) {
	c.Status.Releases = releases
}

// +kubebuilder:object:root=true

// AIGatewayList contains a list of AIGateway
type AIGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AIGateway `json:"items"`
}

// DSCAIGateway contains all the configuration exposed in DSC instance for AIGateway component
type DSCAIGateway struct {
	common.ManagementSpec `json:",inline"`
	AIGatewayCommonSpec   `json:",inline"`
}

// DSCAIGatewayStatus contains the observed state of AIGateway exposed in the DSC
type DSCAIGatewayStatus struct {
	common.ManagementSpec `json:",inline"`
	*AIGatewayCommonStatus `json:",inline"`
}

func init() {
	SchemeBuilder.Register(&AIGateway{}, &AIGatewayList{})
}
