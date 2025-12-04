package v1alpha1

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	MlFlowOperatorComponentName = "mlflowoperator"
	// value should match whats set in the XValidation below
	MlFlowOperatorInstanceName = "default-" + "mlflowoperator"
	MlFlowOperatorKind         = "MlFlowOperator"
)

type MlFlowOperatorCommonSpec struct {
}

type MlFlowOperatorSpec struct {
	MlFlowOperatorCommonSpec `json:",inline"`
}

// MlFlowOperatorCommonStatus defines the shared observed state of MlFlowOperator
type MlFlowOperatorCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// MlFlowOperatorStatus defines the observed state of MlFlowOperator
type MlFlowOperatorStatus struct {
	common.Status              `json:",inline"`
	MlFlowOperatorCommonStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-mlflowoperator'",message="MlFlowOperator name must be default-mlflowoperator"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// MlFlowOperator is the Schema for the MlFlowOperators API
type MlFlowOperator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MlFlowOperatorSpec   `json:"spec,omitempty"`
	Status MlFlowOperatorStatus `json:"status,omitempty"`
}

func (c *MlFlowOperator) GetStatus() *common.Status {
	return &c.Status.Status
}

func (c *MlFlowOperator) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *MlFlowOperator) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

func (c *MlFlowOperator) GetReleaseStatus() *[]common.ComponentRelease {
	return &c.Status.Releases
}

func (c *MlFlowOperator) SetReleaseStatus(releases []common.ComponentRelease) {
	c.Status.Releases = releases
}

// +kubebuilder:object:root=true

// MlFlowOperatorList contains a list of MlFlowOperator
type MlFlowOperatorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MlFlowOperator `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MlFlowOperator{}, &MlFlowOperatorList{})
}

type DSCMlFlowOperator struct {
	common.ManagementSpec    `json:",inline"`
	MlFlowOperatorCommonSpec `json:",inline"`
}

// DSCMlFlowOperatorStatus contains the observed state of the MlFlowOperator exposed in the DSC instance
type DSCMlFlowOperatorStatus struct {
	common.ManagementSpec       `json:",inline"`
	*MlFlowOperatorCommonStatus `json:",inline"`
}
