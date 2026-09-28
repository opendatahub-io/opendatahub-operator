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
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
)

const (
	GatewayServiceName = "gateway"
	// GatewayConfigName is the name of the GatewayConfig instance singleton.
	// value should match what's set in the XValidation below
	GatewayConfigName = "default-gateway"
	GatewayConfigKind = "GatewayConfig"

	DefaultGatewayListenerName       = "https"
	LegacyGatewayListenerName        = "https-legacy"
	DefaultGatewayListenerPort int32 = 443
)

// IngressMode defines how the Gateway exposes its endpoints externally.
// +kubebuilder:validation:Enum=OcpRoute;LoadBalancer
type IngressMode string

const (
	// IngressModeOcpRoute uses ClusterIP service with OpenShift Routes (OpenShift only).
	IngressModeOcpRoute IngressMode = "OcpRoute"
	// IngressModeLoadBalancer uses a LoadBalancer service type.
	// This requires a load balancer provider (cloud or MetalLB).
	IngressModeLoadBalancer IngressMode = "LoadBalancer"
)

const (
	AdditionalIngressListenerReadyConditionType       = "ListenerReady"
	AdditionalIngressRouteAdmittedConditionType       = "RouteAdmitted"
	AdditionalIngressAuthenticationReadyConditionType = "AuthenticationReady"
	AdditionalIngressReadyConditionType               = "Ready"
	AdditionalIngressReconciliationPendingReason      = "ReconciliationPending"
)

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = (*GatewayConfig)(nil)

// GatewayConfigSpec defines the desired state of GatewayConfig
type GatewayConfigSpec struct {
	// IngressMode specifies how the Gateway is exposed externally.
	// "OcpRoute" uses ClusterIP with OpenShift Routes (OpenShift only).
	// "LoadBalancer" uses a LoadBalancer service type (requires cloud or MetalLB).
	// +optional
	IngressMode IngressMode `json:"ingressMode,omitempty"`

	// OIDC configuration (used when cluster is in OIDC authentication mode)
	// +optional
	OIDC *OIDCConfig `json:"oidc,omitempty"`

	// Certificate specifies configuration of the TLS certificate securing communication for the gateway.
	// +optional
	Certificate *infrav1.CertificateSpec `json:"certificate,omitempty"`

	// Domain specifies the host name for intercepting incoming requests.
	// Most likely, you will want to use a wildcard name, like *.example.com.
	// If not set, the cluster's default ingress domain is used (when available).
	// On Kubernetes clusters without a discoverable ingress domain, this field is required.
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

	// ProviderCASecretName is the name of the secret containing the CA certificate for the authentication provider.
	// Used when the OAuth/OIDC provider uses a self-signed or custom CA certificate.
	// Secret must exist in the gateway namespace and contain a 'ca.crt' key with the PEM-encoded CA certificate.
	// +optional
	ProviderCASecretName string `json:"providerCASecretName,omitempty"`

	// VerifyProviderCertificate controls TLS certificate verification for the authentication provider.
	// When true (default), certificates are verified against the system trust store and providerCASecretName.
	// When false, certificate verification is disabled (development/testing only).
	// WARNING: Setting this to false disables security and should only be used in non-production environments.
	// For production use with self-signed certificates, use ProviderCASecretName instead.
	// +optional
	// +kubebuilder:default=true
	VerifyProviderCertificate *bool `json:"verifyProviderCertificate,omitempty"`

	// EnableK8sTokenValidation enables Kubernetes service account token validation via TokenReview API.
	// When enabled, kube-auth-proxy validates bearer tokens as service account tokens alongside OAuth/OIDC authentication.
	// This allows service accounts to authenticate via bearer tokens while human users authenticate via OAuth/OIDC.
	// +optional
	// +kubebuilder:default=true
	EnableK8sTokenValidation *bool `json:"enableK8sTokenValidation,omitempty"`

	// TokenReview configures the rate limiting and caching behavior of Kubernetes TokenReview API calls
	// used for service account token validation.
	// If not set, kube-auth-proxy uses built-in defaults (QPS=50, Burst=100, CacheTTL=10s).
	// These settings only take effect when EnableK8sTokenValidation is true.
	// +optional
	TokenReview *TokenReviewConfig `json:"tokenReview,omitempty"`

	// AdditionalIngresses defines additional listeners on the managed Gateway.
	// Authentication and scaling fields are defined by the per-ingress auth contract.
	// +optional
	AdditionalIngresses AdditionalIngresses `json:"additionalIngresses,omitempty"`
}

// ValidateAdditionalIngresses performs runtime validation for callers that
// construct GatewayConfig objects without API-server admission.
func (s GatewayConfigSpec) ValidateAdditionalIngresses() error {
	return s.AdditionalIngresses.Validate(s.IngressMode)
}

// AdditionalIngresses is the collection of additional Gateway listener definitions.
// +listType=map
// +listMapKey=name
type AdditionalIngresses []AdditionalIngress

// Validate performs runtime validation for additional ingress definitions.
func (ingresses AdditionalIngresses) Validate(ingressMode IngressMode) error {
	if len(ingresses) > 0 && ingressMode != IngressModeOcpRoute {
		return fmt.Errorf("additional ingresses require %s ingress mode", IngressModeOcpRoute)
	}

	seenNames := make(map[string]struct{}, len(ingresses))
	seenHostnames := make(map[string]string, len(ingresses))
	seenPorts := map[int32]string{DefaultGatewayListenerPort: DefaultGatewayListenerName}
	for _, ingress := range ingresses {
		if errs := validation.IsDNS1123Label(ingress.Name); len(errs) > 0 {
			return fmt.Errorf("additional ingress %q has invalid name: %s", ingress.Name, errs[0])
		}
		if ingress.Name == DefaultGatewayListenerName || ingress.Name == LegacyGatewayListenerName {
			return fmt.Errorf("additional ingress %q uses reserved listener name", ingress.Name)
		}
		if _, found := seenNames[ingress.Name]; found {
			return fmt.Errorf("additional ingresses contain duplicate name %q", ingress.Name)
		}
		seenNames[ingress.Name] = struct{}{}

		if errs := validation.IsDNS1123Subdomain(ingress.Hostname); len(errs) > 0 {
			return fmt.Errorf("additional ingress %q has invalid hostname %q: %s", ingress.Name, ingress.Hostname, errs[0])
		}
		hostname := strings.ToLower(strings.TrimSuffix(ingress.Hostname, "."))
		if existingName, found := seenHostnames[hostname]; found {
			return fmt.Errorf("additional ingress %q hostname %q conflicts with %q", ingress.Name, ingress.Hostname, existingName)
		}
		seenHostnames[hostname] = ingress.Name
		if errs := validation.IsDNS1035Label(ingress.IngressControllerName); len(errs) > 0 {
			return fmt.Errorf("additional ingress %q has invalid IngressController name %q: %s", ingress.Name, ingress.IngressControllerName, errs[0])
		}
		if len(ingress.RouteLabels) == 0 {
			return fmt.Errorf("additional ingress %q must define route labels", ingress.Name)
		}
		for key, value := range ingress.RouteLabels {
			if errs := validation.IsQualifiedName(key); len(errs) > 0 {
				return fmt.Errorf("additional ingress %q has invalid route label key %q: %s", ingress.Name, key, errs[0])
			}
			if errs := validation.IsValidLabelValue(value); len(errs) > 0 {
				return fmt.Errorf("additional ingress %q has invalid route label value for %q: %s", ingress.Name, key, errs[0])
			}
		}

		if ingress.ListenerPort < 1 || ingress.ListenerPort > 65535 {
			return fmt.Errorf("additional ingress %q has invalid listener port %d", ingress.Name, ingress.ListenerPort)
		}
		if existingName, found := seenPorts[ingress.ListenerPort]; found {
			return fmt.Errorf("additional ingress %q listener port %d conflicts with %q", ingress.Name, ingress.ListenerPort, existingName)
		}
		seenPorts[ingress.ListenerPort] = ingress.Name
	}
	return nil
}

// AdditionalIngress defines topology for an additional Gateway listener.
// +kubebuilder:object:generate=true
type AdditionalIngress struct {
	// Name is the stable identity of this ingress and the Gateway listener name.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Name string `json:"name"`

	// Hostname is the externally visible hostname for this ingress.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=253
	Hostname string `json:"hostname"`

	// ListenerPort is the stable internal port used by this Gateway listener.
	// It is immutable after the ingress is created.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="ListenerPort is immutable"
	ListenerPort int32 `json:"listenerPort"`

	// IngressControllerName identifies the OpenShift IngressController that admits the bridge Route.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z]([-a-z0-9]*[a-z0-9])?$`
	IngressControllerName string `json:"ingressControllerName"`

	// RouteLabels are applied to the bridge Route and matched against the target
	// IngressController route selector.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinProperties=1
	RouteLabels map[string]string `json:"routeLabels"`
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
	// OIDC issuer URL. Must be an https URL with a non-empty host and no query or
	// fragment component (an OIDC issuer identifier has neither).
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:Format=uri
	// +kubebuilder:validation:Pattern=`^https://[^?#\s]+$`
	// +kubebuilder:validation:XValidation:rule="url(self).getHostname() != ''",message="issuerURL must have a non-empty host"
	IssuerURL string `json:"issuerURL"`

	// OIDC client ID
	// +kubebuilder:validation:Required
	ClientID string `json:"clientID"`

	// Reference to secret containing client secret
	// +kubebuilder:validation:Required
	ClientSecretRef corev1.SecretKeySelector `json:"clientSecretRef"`

	// Namespace where the client secret is located.
	// If unset, defaults to the gateway namespace.
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

// TokenReviewConfig defines rate limiting and caching settings for TokenReview API calls.
type TokenReviewConfig struct {
	// QPS is the maximum queries per second to the Kubernetes API for TokenReview calls.
	// Higher values allow more concurrent token validation requests.
	// If not set, kube-auth-proxy uses its built-in default (50).
	// +optional
	// +kubebuilder:validation:Minimum=1
	QPS *int32 `json:"qps,omitempty"`

	// Burst is the maximum burst of requests to the Kubernetes API for TokenReview calls.
	// Should be equal to or greater than QPS.
	// If not set, kube-auth-proxy uses its built-in default (100).
	// +optional
	// +kubebuilder:validation:Minimum=1
	Burst *int32 `json:"burst,omitempty"`

	// CacheTTL is how long validated token results are cached before re-validation (e.g., "10s", "30s").
	// If not set, kube-auth-proxy uses its built-in default (10s).
	// +optional
	CacheTTL *metav1.Duration `json:"cacheTTL,omitempty"`
}

// GatewayConfigStatus defines the observed state of GatewayConfig
type GatewayConfigStatus struct {
	common.Status `json:",inline"`
	// Domain is the computed gateway domain (subdomain + cluster domain or default)
	// This is the single source of truth for the gateway domain used by all components
	Domain string `json:"domain,omitempty"`

	// AdditionalIngresses contains configured additional ingresses, including entries that are not ready.
	// +optional
	// +listType=map
	// +listMapKey=name
	AdditionalIngresses []AdditionalIngressStatus `json:"additionalIngresses,omitempty"`
}

// AdditionalIngressStatus reports readiness for one additional ingress.
// +kubebuilder:object:generate=true
type AdditionalIngressStatus struct {
	// Name is the stable identity of the configured ingress.
	Name string `json:"name"`

	// Hostname is the configured externally visible hostname.
	Hostname string `json:"hostname"`

	// Conditions report independent listener, Route, authentication, and aggregate readiness.
	// +optional
	// +patchStrategy=merge
	// +patchMergeKey=type
	// +listType=map
	// +listMapKey=type
	Conditions []common.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
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
