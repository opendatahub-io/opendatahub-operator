package v1alpha1

import (
	frameworkapi "github.com/opendatahub-io/odh-platform-utilities/framework/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	SyntheticModuleKind         = "SyntheticModule"
	SyntheticModuleInstanceName = "default-synthetic"

	FaultAnnotationKey            = "test.synthetic.platform.opendatahub.io/fault"
	FaultModeReadyFalse           = "ready-false"
	FaultModeDegraded             = "degraded"
	FaultModeStaleGeneration      = "stale-generation"
	FaultModeStalePlatformRelease = "stale-platform-release"
	FaultModeHoldFinalizer        = "hold-finalizer"
)

// SyntheticModuleSpec contains a harmless value projected into an owned
// ConfigMap by the reference controller.
type SyntheticModuleSpec struct {
	Message string `json:"message,omitempty"`
}

// SyntheticModuleStatus is the standard module status consumed by the
// platform operator.
type SyntheticModuleStatus struct {
	frameworkapi.Status                 `json:",inline"`
	frameworkapi.ComponentReleaseStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:subresource:status
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-synthetic'",message="SyntheticModule is a singleton and must be named default-synthetic"
type SyntheticModule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SyntheticModuleSpec   `json:"spec,omitempty"`
	Status SyntheticModuleStatus `json:"status,omitempty"`
}

func (m *SyntheticModule) GetStatus() *frameworkapi.Status {
	return &m.Status.Status
}

func (m *SyntheticModule) GetConditions() []frameworkapi.Condition {
	return m.Status.Conditions
}

func (m *SyntheticModule) SetConditions(conditions []frameworkapi.Condition) {
	m.Status.SetConditions(conditions)
}

// +kubebuilder:object:root=true
type SyntheticModuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SyntheticModule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SyntheticModule{}, &SyntheticModuleList{})
}
