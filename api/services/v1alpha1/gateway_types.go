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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	GatewayServiceName  = "gateway"
	GatewayInstanceName = "gateway"
	GatewayKind         = "Gateway"
)

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = (*Gateway)(nil)

// GatewaySpec defines the desired state of Gateway
type GatewaySpec struct {
	// Domain is the domain name for the gateway
	// +optional
	Domain string `json:"domain,omitempty"`

	// Certificate contains certificate configuration for TLS
	// +optional
	Certificate *GatewayCertificate `json:"certificate,omitempty"`
}

// GatewayCertificateType defines the type of certificate management
type GatewayCertificateType string

const (
	// CertManagerCertificate uses cert-manager to automatically generate and manage certificates
	CertManagerCertificate GatewayCertificateType = "CertManager"
	// ProvidedCertificate uses a pre-existing secret with certificate data
	ProvidedCertificate GatewayCertificateType = "Provided"
	// SelfSignedCertificate generates a self-signed certificate
	SelfSignedCertificate GatewayCertificateType = "SelfSigned"
)

// GatewayCertificate defines TLS certificate configuration for the gateway
type GatewayCertificate struct {
	// Type of certificate management
	// +kubebuilder:validation:Enum=CertManager;Provided;SelfSigned
	// +kubebuilder:default=CertManager
	Type GatewayCertificateType `json:"type,omitempty"`

	// SecretName is the name of the secret containing the certificate
	// When Type is CertManager, this will be the name of the secret created by cert-manager
	// When Type is Provided, this should be the name of an existing secret
	// +optional
	SecretName string `json:"secretName,omitempty"`

	// IssuerRef is a reference to the cert-manager Issuer or ClusterIssuer
	// Only used when Type is CertManager
	// +optional
	IssuerRef *GatewayIssuerRef `json:"issuerRef,omitempty"`
}

// GatewayIssuerRef references a cert-manager Issuer or ClusterIssuer
type GatewayIssuerRef struct {
	// Name of the Issuer or ClusterIssuer
	Name string `json:"name"`

	// Kind of the issuer (Issuer or ClusterIssuer)
	// +kubebuilder:validation:Enum=Issuer;ClusterIssuer
	// +kubebuilder:default=ClusterIssuer
	Kind string `json:"kind,omitempty"`

	// Group of the issuer
	// +kubebuilder:default=cert-manager.io
	Group string `json:"group,omitempty"`
}

// GatewayStatus defines the observed state of Gateway
type GatewayStatus struct {
	common.Status `json:",inline"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'gateway'",message="Gateway name must be gateway"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// Gateway is the Schema for the gateways API
type Gateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GatewaySpec   `json:"spec,omitempty"`
	Status GatewayStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// GatewayList contains a list of Gateway
type GatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Gateway `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Gateway{}, &GatewayList{})
}

// GetDevFlags returns the DevFlags of the Gateway
func (g *Gateway) GetDevFlags() *common.DevFlags {
	return nil
}

// SetDevFlags sets the DevFlags of the Gateway
func (g *Gateway) SetDevFlags(devFlags *common.DevFlags) {
	// Gateway doesn't support dev flags currently
}

// GetStatus returns the Status of the Gateway
func (g *Gateway) GetStatus() *common.Status {
	return &g.Status.Status
}

// GetConditions returns the conditions of the Gateway
func (g *Gateway) GetConditions() []common.Condition {
	return g.Status.GetConditions()
}

// SetConditions sets the conditions of the Gateway
func (g *Gateway) SetConditions(conditions []common.Condition) {
	g.Status.SetConditions(conditions)
}
