package v1alpha1

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	MCPLifecycleOperatorComponentName = "mcplifecycleoperator"

	MCPLifecycleOperatorInstanceName = "default-mcplifecycleoperator"

	MCPLifecycleOperatorKind = "MCPLifecycleOperator"
)

var _ common.PlatformObject = (*MCPLifecycleOperator)(nil)

type MCPLifecycleOperatorCommonSpec struct{}

type MCPLifecycleOperatorSpec struct {
	MCPLifecycleOperatorCommonSpec `json:",inline"`
}

type MCPLifecycleOperatorCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

type MCPLifecycleOperatorStatus struct {
	common.Status                    `json:",inline"`
	MCPLifecycleOperatorCommonStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-mcplifecycleoperator'",message="MCPLifecycleOperator name must be default-mcplifecycleoperator"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

type MCPLifecycleOperator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MCPLifecycleOperatorSpec   `json:"spec,omitempty"`
	Status MCPLifecycleOperatorStatus `json:"status,omitempty"`
}

func (c *MCPLifecycleOperator) GetStatus() *common.Status {
	return &c.Status.Status
}

func (c *MCPLifecycleOperator) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *MCPLifecycleOperator) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

func (c *MCPLifecycleOperator) GetReleaseStatus() *[]common.ComponentRelease {
	return &c.Status.Releases
}

func (c *MCPLifecycleOperator) SetReleaseStatus(releases []common.ComponentRelease) {
	c.Status.Releases = releases
}

// +kubebuilder:object:root=true

type MCPLifecycleOperatorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MCPLifecycleOperator `json:"items"`
}

func init() {
	SchemeBuilder.Register(&MCPLifecycleOperator{}, &MCPLifecycleOperatorList{})
}

type DSCMCPLifecycleOperator struct {
	common.ManagementSpec          `json:",inline"`
	MCPLifecycleOperatorCommonSpec `json:",inline"`
}

type DSCMCPLifecycleOperatorStatus struct {
	common.ManagementSpec             `json:",inline"`
	*MCPLifecycleOperatorCommonStatus `json:",inline"`
}
