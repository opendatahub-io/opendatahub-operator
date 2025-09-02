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
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	GatewayServiceName = "gateway"
	// GatewayInstanceName the name of the Gateway instance singleton.
	// value should match whats set in the XValidation below
	GatewayInstanceName = "default-gateway"
	GatewayKind         = "Gateway"
)

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = (*Gateway)(nil)

// GatewaySpec defines the desired state of Gateway
type GatewaySpec struct {
	// Authentication configuration
	// +optional
	Auth GatewayAuthSpec `json:"auth"`

	// Certificate management
	// +optional
	Certificates GatewayCertSpec `json:"certificates"`

	// Domain configuration for the gateway
	// +optional
	Domain string `json:"domain,omitempty"`

	// Namespace where the gateway resources should be deployed
	// +kubebuilder:default="openshift-ingress"
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// GatewayAuthSpec defines authentication configuration for the gateway
type GatewayAuthSpec struct {
	// Authentication mode: "openshift-oauth" | "oidc" | "auto"
	// +kubebuilder:validation:Enum=openshift-oauth;oidc;auto
	// +kubebuilder:default="auto"
	// +optional
	Mode string `json:"mode,omitempty"`

	// OIDC configuration (required when mode="oidc")
	// +optional
	OIDC *OIDCConfig `json:"oidc,omitempty"`
}

// OIDCConfig defines OIDC provider configuration
type OIDCConfig struct {
	// OIDC issuer URL
	// +kubebuilder:validation:Required
	IssuerURL string `json:"issuerURL"`

	// Reference to secret containing clientID and clientSecret
	// +kubebuilder:validation:Required
	ClientSecretRef corev1.SecretKeySelector `json:"clientSecretRef"`
}

// GatewayCertSpec defines certificate management configuration
type GatewayCertSpec struct {
	// Type of certificate management: "user-provided" | "cert-manager" | "openshift-service-ca"
	// +kubebuilder:validation:Enum=user-provided;cert-manager;openshift-service-ca
	// +kubebuilder:default="openshift-service-ca"
	// +optional
	Type string `json:"type,omitempty"`

	// Reference to user-provided certificate secret (when type="user-provided")
	// +optional
	SecretRef *corev1.SecretReference `json:"secretRef,omitempty"`

	// IssuerRef for cert-manager certificates (when type="cert-manager")
	// +optional
	IssuerRef *cmmeta.ObjectReference `json:"issuerRef,omitempty"`
}

// GatewayStatus defines the observed state of Gateway
type GatewayStatus struct {
	common.Status `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-gateway'",message="Gateway name must be default-gateway"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// Gateway is the Schema for the gateways API
type Gateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GatewaySpec   `json:"spec,omitempty"`
	Status GatewayStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GatewayList contains a list of Gateway
type GatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Gateway `json:"items"`
}

func (m *Gateway) GetStatus() *common.Status {
	return &m.Status.Status
}

func (c *Gateway) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *Gateway) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

func init() {
	SchemeBuilder.Register(&Gateway{}, &GatewayList{})
}
