/*
Copyright 2025.

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
	GatewayConfigName = "default-gateway"
	GatewayConfigKind = "GatewayConfig"
)

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = (*GatewayConfig)(nil)

// GatewayConfigSpec defines the desired state of GatewayConfig
type GatewayConfigSpec struct {
	// OIDC configuration (used when cluster is in OIDC authentication mode)
	// +optional
	OIDC *OIDCConfig `json:"oidc,omitempty"`

	// Certificate specifies configuration of the TLS certificate securing communication for the gateway.
	// +optional
	Certificate *infrav1.CertificateSpec `json:"certificate,omitempty"`

	// Domain specifies the host name for intercepting incoming requests.
	// Most likely, you will want to use a wildcard name, like *.example.com.
	// If not set, the domain of the OpenShift Ingress is used.
	// If you choose to generate a certificate, this is the domain used for the certificate request.
	// Example: *.example.com, example.com, apps.example.com
	// +optional
	// +kubebuilder:validation:Pattern=`^(\*\.)?([a-z0-9]([-a-z0-9]*[a-z0-9])?\.)*[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Domain string `json:"domain,omitempty"`

	// Subdomain configuration for the GatewayConfig
	// Example: my-gateway, custom-gateway
	// +optional
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^([a-z0-9]([-a-z0-9]*[a-z0-9])?)$`
	Subdomain string `json:"subdomain,omitempty"`

	// Cookie configuration (applies to both OIDC and OpenShift OAuth)
	// +optional
	Cookie CookieConfig `json:"cookie,omitempty"` // not pointer to make defaults clear

	// AuthTimeout is the duration Envoy waits for auth proxy responses.
	// Requests timeout with 403 if exceeded.
	// Deprecated: Use AuthProxyTimeout instead.
	// +optional
	// +kubebuilder:validation:Pattern=`^([0-9]+(\.[0-9]+)?(ns|us|µs|ms|s|m|h))+$`
	AuthTimeout string `json:"authTimeout,omitempty"`

	// AuthProxyTimeout defines the timeout for external authorization service calls (e.g., "5s", "10s")
	// This controls how long Envoy waits for a response from the authentication proxy before timing out 403 response.
	// +optional
	AuthProxyTimeout metav1.Duration `json:"authProxyTimeout,omitempty"`

	// NetworkPolicy configuration for kube-auth-proxy
	// +optional
	NetworkPolicy *NetworkPolicyConfig `json:"networkPolicy,omitempty"`
}

// NetworkPolicyConfig defines network policy configuration for kube-auth-proxy.
// When nil or when Ingress is nil, NetworkPolicy ingress rules are enabled by default
// to restrict access to kube-auth-proxy pods.
type NetworkPolicyConfig struct {
	// Ingress defines ingress NetworkPolicy rules.
	// When nil, ingress rules are applied by default (allows traffic from Gateway pods and monitoring namespaces).
	// When specified, Enabled must be set to true to apply rules or false to skip NetworkPolicy creation.
	// Set Enabled=false only in development environments or when using alternative network security controls.
	// +optional
	Ingress *IngressPolicyConfig `json:"ingress,omitempty"`
}

// IngressPolicyConfig defines ingress NetworkPolicy rules
type IngressPolicyConfig struct {
	// Enabled determines whether ingress rules are applied.
	// When true, creates NetworkPolicy allowing traffic only from Gateway pods and monitoring namespaces.
	// +kubebuilder:validation:Required
	Enabled bool `json:"enabled"`
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

	// Namespace where the client secret is located
	// If not specified, defaults to openshift-ingress
	// +optional
	SecretNamespace string `json:"secretNamespace,omitempty"`
}

// CookieConfig defines cookie settings for OAuth2 proxy
type CookieConfig struct {
	// Expire duration for OAuth2 proxy session cookie (e.g., "24h", "8h")
	// This controls how long the session cookie is valid before requiring re-authentication.
	// +optional
	// +kubebuilder:default="24h"
	Expire metav1.Duration `json:"expire,omitempty"`

	// Refresh duration for OAuth2 proxy to refresh access tokens (e.g., "2h", "1h", "30m")
	// This must be LESS than the OIDC provider's Access Token Lifespan to avoid token expiration.
	// For example, if Keycloak Access Token Lifespan is 1 hour, set this to "30m" or "45m".
	// +optional
	// +kubebuilder:default="1h"
	Refresh metav1.Duration `json:"refresh,omitempty"`
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
