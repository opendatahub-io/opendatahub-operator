package v1alpha1

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	MLflowOperatorComponentName = "mlflowoperator"
	// value should match whats set in the XValidation below
	MLflowOperatorInstanceName = "default-" + "mlflowoperator"
	MLflowOperatorKind         = "MLflowOperator"
)

type MLflowOperatorCommonSpec struct {
}

type MLflowOperatorSpec struct {
	MLflowOperatorCommonSpec `json:",inline"`
}

// MLflowOperatorCommonStatus defines the shared observed state of MLflowOperator
type MLflowOperatorCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// MLflowOperatorStatus defines the observed state of MLflowOperator
type MLflowOperatorStatus struct {
	common.Status              `json:",inline"`
	MLflowOperatorCommonStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-mlflowoperator'",message="MLflowOperator name must be default-mlflowoperator"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// MLflowOperator is the Schema for the MLflowOperators API
type MLflowOperator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MLflowOperatorSpec   `json:"spec,omitempty"`
	Status MLflowOperatorStatus `json:"status,omitempty"`
}

func (c *MLflowOperator) GetStatus() *common.Status {
	return &c.Status.Status
}

func (c *MLflowOperator) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *MLflowOperator) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

func (c *MLflowOperator) GetReleaseStatus() *[]common.ComponentRelease {
	return &c.Status.Releases
}

func (c *MLflowOperator) SetReleaseStatus(releases []common.ComponentRelease) {
	c.Status.Releases = releases
}

// +kubebuilder:object:root=true

// MLflowOperatorList contains a list of MLflowOperator
type MLflowOperatorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MLflowOperator `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MLflowOperator{}, &MLflowOperatorList{})
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
