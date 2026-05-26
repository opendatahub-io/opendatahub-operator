package v1alpha1

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	BatchGatewayComponentName = "batchgateway"
	// value should match whats set in the XValidation below
	BatchGatewayInstanceName = "default-" + "batchgateway"
	BatchGatewayKind         = "BatchGateway"
)

type BatchGatewayCommonSpec struct {
}

type BatchGatewaySpec struct {
	BatchGatewayCommonSpec `json:",inline"`
}

// BatchGatewayCommonStatus defines the shared observed state of BatchGateway
type BatchGatewayCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// BatchGatewayStatus defines the observed state of BatchGateway
type BatchGatewayStatus struct {
	common.Status            `json:",inline"`
	BatchGatewayCommonStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-batchgateway'",message="BatchGateway name must be default-batchgateway"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// BatchGateway is the Schema for the batchgateways API
type BatchGateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BatchGatewaySpec   `json:"spec,omitempty"`
	Status BatchGatewayStatus `json:"status,omitempty"`
}

func (c *BatchGateway) GetStatus() *common.Status {
	return &c.Status.Status
}

func (c *BatchGateway) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *BatchGateway) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

func (c *BatchGateway) GetReleaseStatus() *[]common.ComponentRelease {
	return &c.Status.Releases
}

func (c *BatchGateway) SetReleaseStatus(releases []common.ComponentRelease) {
	c.Status.Releases = releases
}

// +kubebuilder:object:root=true

// BatchGatewayList contains a list of BatchGateway
type BatchGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BatchGateway `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BatchGateway{}, &BatchGatewayList{})
}

type DSCBatchGateway struct {
	common.ManagementSpec  `json:",inline"`
	BatchGatewayCommonSpec `json:",inline"`
}

// DSCBatchGatewayStatus contains the observed state of the BatchGateway exposed in the DSC instance
type DSCBatchGatewayStatus struct {
	common.ManagementSpec     `json:",inline"`
	*BatchGatewayCommonStatus `json:",inline"`
}
