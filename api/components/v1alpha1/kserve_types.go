/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
)

const (
	KserveComponentName = "kserve"
	// value should match what's set in the XValidation below
	KserveInstanceName = "default-" + KserveComponentName
	KserveKind         = "Kserve"
)

// +kubebuilder:validation:Pattern=`^(Serverless|RawDeployment)$`
type DefaultDeploymentMode string

const (
	// Serverless will be used as the default deployment mode for Kserve. This requires Serverless and ServiceMesh operators configured as dependencies.
	Serverless DefaultDeploymentMode = "Serverless"
	// RawDeployment will be used as the default deployment mode for Kserve.
	RawDeployment DefaultDeploymentMode = "RawDeployment"
)

// +kubebuilder:validation:Pattern=`^(Headless|Headed)$`
type RawServiceConfig string

const (
	KserveRawHeadless RawServiceConfig = "Headless"
	KserveRawHeaded   RawServiceConfig = "Headed"
)

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = (*Kserve)(nil)

// KserveCommonSpec spec defines the shared desired state of Kserve
type KserveCommonSpec struct {
	common.DevFlagsSpec `json:",inline"`
	// Serving configures the KNative-Serving stack used for model serving. A Service
	// Mesh (Istio) is prerequisite, since it is used as networking layer.
	Serving infrav1.ServingSpec `json:"serving,omitempty"`
	// Configures the default deployment mode for Kserve. This can be set to 'Serverless' or 'RawDeployment'.
	// The value specified in this field will be used to set the default deployment mode in the 'inferenceservice-config' configmap for Kserve.
	// This field is optional. If no default deployment mode is specified, Kserve will use Serverless mode.
	// +kubebuilder:validation:Enum=Serverless;RawDeployment
	DefaultDeploymentMode DefaultDeploymentMode `json:"defaultDeploymentMode,omitempty"`
	// Configures the type of service that is created for InferenceServices using RawDeployment.
	// The values for RawDeploymentServiceConfig can be "Headless" or "Headed".
	// Headless : sets "ServiceClusterIPNone = true" in the 'inferenceservice-config' configmap for Kserve.
	// Headed : sets "ServiceClusterIPNone = false" in the 'inferenceservice-config' configmap for Kserve.
	// +kubebuilder:validation:Enum=Headless;Headed
	// +kubebuilder:default=Headless
	RawDeploymentServiceConfig RawServiceConfig `json:"rawDeploymentServiceConfig,omitempty"`
	// Configures and enables NVIDIA NIM integration
	NIM NimSpec `json:"nim,omitempty"`
}

// nimSpec enables NVIDIA NIM integration
type NimSpec struct {
	// +kubebuilder:validation:Enum=Managed;Removed
	// +kubebuilder:default=Managed
	ManagementState operatorv1.ManagementState `json:"managementState,omitempty"`
}

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// KserveSpec defines the desired state of Kserve
type KserveSpec struct {
	// kserve spec exposed to DSC api
	KserveCommonSpec `json:",inline"`
	// kserve spec exposed only to internal api
}

// KserveCommonStatus defines the shared observed state of Kserve
type KserveCommonStatus struct {
	// DefaultDeploymentMode is the value of the defaultDeploymentMode field
	// as read from the "deploy" JSON in the inferenceservice-config ConfigMap
	DefaultDeploymentMode         string                     `json:"defaultDeploymentMode,omitempty"`
	ServerlessMode                operatorv1.ManagementState `json:"serverlessMode,omitempty"`
	common.ComponentReleaseStatus `json:",inline"`
}

// KserveStatus defines the observed state of Kserve
type KserveStatus struct {
	common.Status      `json:",inline"`
	KserveCommonStatus `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-kserve'",message="Kserve name must be default-kserve"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// Kserve is the Schema for the kserves API
type Kserve struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KserveSpec   `json:"spec,omitempty"`
	Status KserveStatus `json:"status,omitempty"`
}

func (c *Kserve) GetDevFlags() *common.DevFlags {
	return c.Spec.DevFlags
}

func (c *Kserve) GetStatus() *common.Status {
	return &c.Status.Status
}

func (c *Kserve) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *Kserve) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

func (c *Kserve) GetReleaseStatus() *[]common.ComponentRelease {
	return &c.Status.Releases
}

func (c *Kserve) SetReleaseStatus(releases []common.ComponentRelease) {
	c.Status.Releases = releases
}

// +kubebuilder:object:root=true

// KserveList contains a list of Kserve
type KserveList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Kserve `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Kserve{}, &KserveList{})
}

// DSCKserve contains all the configuration exposed in DSC instance for Kserve component
type DSCKserve struct {
	// configuration fields common across components
	common.ManagementSpec `json:",inline"`
	// Kserve specific fields
	KserveCommonSpec `json:",inline"`
}

// DSCKserveStatus contains the observed state of the Kserve exposed in the DSC instance
type DSCKserveStatus struct {
	common.ManagementSpec `json:",inline"`
	*KserveCommonStatus   `json:",inline"`
}
