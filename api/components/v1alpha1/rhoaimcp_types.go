package v1alpha1

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	RhoaiMcpComponentName = "rhoaimcp"
	// value should match whats set in the XValidation below
	RhoaiMcpInstanceName = "default-" + "rhoaimcp"
	RhoaiMcpKind         = "RhoaiMcp"
)

type RhoaiMcpCommonSpec struct {
	// Transport mode for MCP server connections.
	// +kubebuilder:validation:Enum=sse;"streamable-http";stdio
	// +kubebuilder:default=sse
	Transport string `json:"transport,omitempty"`

	// AuthMode for cluster authentication.
	// +kubebuilder:validation:Enum=auto;kubeconfig;token
	// +kubebuilder:default=auto
	AuthMode string `json:"authMode,omitempty"`

	// ReadOnlyMode disables all write operations when true.
	// +kubebuilder:default=false
	ReadOnlyMode bool `json:"readOnlyMode,omitempty"`

	// EnableDangerousOperations enables delete operations when true.
	// +kubebuilder:default=false
	EnableDangerousOperations bool `json:"enableDangerousOperations,omitempty"`

	// EnabledPlugins lists which domain plugins to enable.
	// When empty, all plugins are enabled.
	EnabledPlugins []string `json:"enabledPlugins,omitempty"`
}

type RhoaiMcpSpec struct {
	RhoaiMcpCommonSpec `json:",inline"`
}

// RhoaiMcpCommonStatus defines the shared observed state of RhoaiMcp
type RhoaiMcpCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// RhoaiMcpStatus defines the observed state of RhoaiMcp
type RhoaiMcpStatus struct {
	common.Status        `json:",inline"`
	RhoaiMcpCommonStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-rhoaimcp'",message="RhoaiMcp name must be default-rhoaimcp"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// RhoaiMcp is the Schema for the RhoaiMcps API
type RhoaiMcp struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RhoaiMcpSpec   `json:"spec,omitempty"`
	Status RhoaiMcpStatus `json:"status,omitempty"`
}

func (c *RhoaiMcp) GetStatus() *common.Status {
	return &c.Status.Status
}

func (c *RhoaiMcp) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *RhoaiMcp) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

func (c *RhoaiMcp) GetReleaseStatus() *[]common.ComponentRelease {
	return &c.Status.Releases
}

func (c *RhoaiMcp) SetReleaseStatus(releases []common.ComponentRelease) {
	c.Status.Releases = releases
}

// +kubebuilder:object:root=true

// RhoaiMcpList contains a list of RhoaiMcp
type RhoaiMcpList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RhoaiMcp `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RhoaiMcp{}, &RhoaiMcpList{})
}

type DSCRhoaiMcp struct {
	common.ManagementSpec `json:",inline"`
	RhoaiMcpCommonSpec    `json:",inline"`
}

// DSCRhoaiMcpStatus contains the observed state of the RhoaiMcp exposed in the DSC instance
type DSCRhoaiMcpStatus struct {
	common.ManagementSpec `json:",inline"`
	*RhoaiMcpCommonStatus `json:",inline"`
}
