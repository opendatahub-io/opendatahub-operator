package v1alpha1

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// component name
	LlamaStackOperatorComponentName = "llamastackoperator"

	// LlamaStackOperatorInstanceName is the name of the new component instance singleton
	// value should match what is set in the kubebuilder markers for XValidation defined below
	LlamaStackOperatorInstanceName = "default-llamastackoperator"

	// kubernetes kind of the new component
	LlamaStackOperatorKind = "LlamaStackOperator"
)

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = (*LlamaStackOperator)(nil)

// default kubebuilder markers for the new component
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-llamastackoperator'",message="LlamaStackOperator name must be default-llamastackoperator"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// LlamaStackOperator is the Schema for the LlamaStackOperator API
type LlamaStackOperator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LlamaStackOperatorSpec   `json:"spec,omitempty"`
	Status LlamaStackOperatorStatus `json:"status,omitempty"`
}

type LlamaStackOperatorCommonSpec struct {
	// new component spec exposed to DSC api
	common.DevFlagsSpec `json:",inline"`
}

// LlamaStackOperatorSpec defines the desired state of LlamaStackOperator
type LlamaStackOperatorSpec struct {
	LlamaStackOperatorCommonSpec `json:",inline"`
}

// LlamaStackOperatorCommonStatus defines the shared observed state of LlamaStackOperator
type LlamaStackOperatorCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// LlamaStackOperatorStatus defines the observed state of LlamaStackOperator
type LlamaStackOperatorStatus struct {
	common.Status                  `json:",inline"`
	LlamaStackOperatorCommonStatus `json:",inline"`
}

// GetDevFlags returns the component's development flags configuration.
// May return nil if DevFlagsSpec is not set. Callers must nil-check the result
// to avoid null pointer exceptions in reconciler code.
func (c *LlamaStackOperator) GetDevFlags() *common.DevFlags {
	return c.Spec.DevFlags
}

// GetStatus retrieves the status of the LlamaStackOperator component
func (c *LlamaStackOperator) GetStatus() *common.Status {
	return &c.Status.Status
}

func (c *LlamaStackOperator) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *LlamaStackOperator) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

func (c *LlamaStackOperator) GetReleaseStatus() *[]common.ComponentRelease {
	return &c.Status.Releases
}

func (c *LlamaStackOperator) SetReleaseStatus(releases []common.ComponentRelease) {
	c.Status.Releases = releases
}

// +kubebuilder:object:root=true

// LlamaStackOperatorList contains a list of LlamaStackOperator
type LlamaStackOperatorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LlamaStackOperator `json:"items"`
}

// DSCLlamaStackOperatorStatus struct holds the status for the LlamaStackOperator component exposed in the DSC
type DSCLlamaStackOperatorStatus struct {
	common.ManagementSpec           `json:",inline"`
	*LlamaStackOperatorCommonStatus `json:",inline"`
}

// register the defined schemas
func init() {
	SchemeBuilder.Register(&LlamaStackOperator{}, &LlamaStackOperatorList{})
}

// DSCLlamaStackOperator contains all the configuration exposed in DSC instance for LlamaStackOperator component
type DSCLlamaStackOperator struct {
	common.ManagementSpec `json:",inline"`

	LlamaStackOperatorCommonSpec `json:",inline"`
}
