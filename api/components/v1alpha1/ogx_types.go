package v1alpha1

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// component name
	OGXComponentName = "ogx"

	// OGXInstanceName is the name of the new component instance singleton
	// value should match what is set in the kubebuilder markers for XValidation defined below
	OGXInstanceName = "default-ogx"

	// kubernetes kind of the new component
	OGXKind = "OGX"
)

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = (*OGX)(nil)

// default kubebuilder markers for the new component
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,path=ogxs
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-ogx'",message="OGX name must be default-ogx"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// OGX is the Schema for the OGX API
type OGX struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OGXSpec   `json:"spec,omitempty"`
	Status OGXStatus `json:"status,omitempty"`
}

type OGXCommonSpec struct {
	// new component spec exposed to DSC api
}

// OGXSpec defines the desired state of OGX
type OGXSpec struct {
	OGXCommonSpec `json:",inline"`
}

// OGXCommonStatus defines the shared observed state of OGX
type OGXCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// OGXStatus defines the observed state of OGX
type OGXStatus struct {
	common.Status   `json:",inline"`
	OGXCommonStatus `json:",inline"`
}

// GetStatus retrieves the status of the OGX component
func (c *OGX) GetStatus() *common.Status {
	return &c.Status.Status
}

func (c *OGX) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *OGX) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

func (c *OGX) GetReleaseStatus() *[]common.ComponentRelease {
	return &c.Status.Releases
}

func (c *OGX) SetReleaseStatus(releases []common.ComponentRelease) {
	c.Status.Releases = releases
}

// +kubebuilder:object:root=true

// OGXList contains a list of OGX
type OGXList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OGX `json:"items"`
}

// DSCOGX contains all the configuration exposed in DSC instance for OGX component
type DSCOGX struct {
	common.ManagementSpec `json:",inline"`

	OGXCommonSpec `json:",inline"`
}

// DSCOGXStatus struct holds the status for the OGX component exposed in the DSC
type DSCOGXStatus struct {
	common.ManagementSpec `json:",inline"`
	*OGXCommonStatus      `json:",inline"`
}

// register the defined schemas
func init() {
	SchemeBuilder.Register(&OGX{}, &OGXList{})
}
