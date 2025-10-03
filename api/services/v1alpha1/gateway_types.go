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
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	GatewayServiceName = "gateway"
	// GatewayInstanceName the name of the GatewayConfig instance singleton.
	// value should match whats set in the XValidation below
	GatewayInstanceName = "default-gateway"
	GatewayKind         = "GatewayConfig"
)

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = (*GatewayConfig)(nil)

// GatewayConfigSpec defines the desired state of GatewayConfig
type GatewayConfigSpec struct {
	// OIDC configuration (used when cluster is in OIDC authentication mode)
	// +optional
	OIDC *OIDCConfig `json:"oidc,omitempty"`

	// Certificate management
	// +optional
	Certificate *infrav1.CertificateSpec `json:"certificate,omitempty"`

	// Domain configuration for the GatewayConfig
	// Example: apps.example.com
	// +optional
	Domain string `json:"domain,omitempty"`
}

// OIDCConfig defines OIDC provider configuration
type OIDCConfig struct {
	// OIDC issuer URL
	// +kubebuilder:validation:Required
	IssuerURL string `json:"issuerURL"`

	// OIDC client ID
	// +kubebuilder:validation:Required
	ClientID string `json:"clientID"`

	// Reference to secret containing client secret
	// +kubebuilder:validation:Required
	ClientSecretRef corev1.SecretKeySelector `json:"clientSecretRef"`

	// Namespace where the ClientSecretRef is located
	// If not specified, defaults to openshift-ingress
	// +optional
	SecretNamespace string `json:"secretNamespace,omitempty"`
}

// GatewayConfigStatus defines the observed state of GatewayConfig
type GatewayConfigStatus struct {
	common.Status `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-gateway'",message="GatewayConfig name must be default-gateway"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// GatewayConfig is the Schema for the gatewayconfigs API
type GatewayConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GatewayConfigSpec   `json:"spec,omitempty"`
	Status GatewayConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GatewayConfigList contains a list of GatewayConfig
type GatewayConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GatewayConfig `json:"items"`
}

func (m *GatewayConfig) GetStatus() *common.Status {
	return &m.Status.Status
}

func (c *GatewayConfig) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *GatewayConfig) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

func init() {
	SchemeBuilder.Register(&GatewayConfig{}, &GatewayConfigList{})
}
