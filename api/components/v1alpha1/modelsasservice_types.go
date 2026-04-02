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
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

const (
	ModelsAsServiceComponentName = "modelsasservice"
	// value should match whats set in the XValidation below
	ModelsAsServiceInstanceName = "default-" + ModelsAsServiceComponentName
	ModelsAsServiceKind         = "ModelsAsService"
)

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = (*ModelsAsService)(nil)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-modelsasservice'",message="ModelsAsService name must be default-modelsasservice"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// ModelsAsService is the Schema for the modelsasservice API
type ModelsAsService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModelsAsServiceSpec   `json:"spec,omitempty"`
	Status ModelsAsServiceStatus `json:"status,omitempty"`
}

// ModelsAsServiceSpec defines the desired state of ModelsAsService
type ModelsAsServiceSpec struct {
	// GatewayRef specifies which Gateway (Gateway API) to use for exposing model endpoints.
	// If omitted, defaults to openshift-ingress/maas-default-gateway.
	// +kubebuilder:validation:Optional
	GatewayRef GatewayRef `json:"gatewayRef,omitempty"`

	// APIKeys contains configuration for API key management.
	// +kubebuilder:validation:Optional
	APIKeys *APIKeysConfig `json:"apiKeys,omitempty"`

	// ExternalOIDC configures an external OIDC identity provider (e.g. Keycloak, Azure AD)
	// for the maas-api AuthPolicy. When set, the operator patches the AuthPolicy to accept
	// JWTs from the specified issuer alongside OpenShift TokenReview and API key authentication.
	// +kubebuilder:validation:Optional
	ExternalOIDC *ExternalOIDCConfig `json:"externalOIDC,omitempty"`

	// Telemetry contains configuration for telemetry and metrics collection.
	// +kubebuilder:validation:Optional
	Telemetry *TelemetryConfig `json:"telemetry,omitempty"`
}

// ExternalOIDCConfig defines the external OIDC provider settings.
type ExternalOIDCConfig struct {
	// IssuerURL is the OIDC issuer URL (e.g. https://keycloak.example.com/realms/maas).
	// Must serve a .well-known/openid-configuration endpoint over HTTPS.
	// +kubebuilder:validation:MinLength=9
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:Pattern=`^https://\S+$`
	IssuerURL string `json:"issuerUrl"`

	// ClientID is the OAuth2 client ID. Incoming OIDC tokens must have an
	// azp (authorized party) claim matching this value.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +kubebuilder:validation:Pattern=`^\S+$`
	ClientID string `json:"clientId"`

	// TTL is the JWKS cache duration in seconds.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=300
	// +kubebuilder:validation:Minimum=30
	TTL int `json:"ttl,omitempty"`
	// NOTE: For OIDC providers with custom/self-signed CA certificates, configure the CA bundle
	// in DSCInitialization.spec.trustedCABundle.customCABundle. The certconfigmapgenerator controller
	// will create an odh-trusted-ca-bundle ConfigMap in all namespaces (including where Authorino runs),
	// and the operator will configure Authorino to mount it for OIDC validation.
	// See: https://github.com/opendatahub-io/opendatahub-operator/blob/main/docs/DESIGN.md#trusted-ca-bundle
}

// TelemetryConfig defines configuration for telemetry collection.
// Core billing and access control metrics (subscription, cost_center, tier) are always emitted.
type TelemetryConfig struct {
	// Metrics contains configuration for optional metric dimensions/labels.
	// +kubebuilder:validation:Optional
	Metrics *MetricsConfig `json:"metrics,omitempty"`
}

// MetricsConfig defines which dimensions (labels) are captured in telemetry metrics.
// Each dimension can be enabled or disabled to control metric cardinality and storage costs.
// Note: subscription, cost_center, and tier dimensions are always emitted for billing and access control.
type MetricsConfig struct {
	// CaptureOrganization enables the organization_id label on metrics.
	// +kubebuilder:default=true
	// +kubebuilder:validation:Optional
	CaptureOrganization *bool `json:"captureOrganization,omitempty"`

	// CaptureUser enables the user label on metrics.
	// Disabled by default for privacy/GDPR compliance.
	// +kubebuilder:default=false
	// +kubebuilder:validation:Optional
	CaptureUser *bool `json:"captureUser,omitempty"`

	// CaptureGroup enables the group label on metrics for team-based chargeback.
	// Note: This is a high-cardinality dimension and is disabled by default.
	// +kubebuilder:default=false
	// +kubebuilder:validation:Optional
	CaptureGroup *bool `json:"captureGroup,omitempty"`

	// CaptureModelUsage enables the model label on metrics.
	// +kubebuilder:default=true
	// +kubebuilder:validation:Optional
	CaptureModelUsage *bool `json:"captureModelUsage,omitempty"`
}

// APIKeysConfig defines configuration options for API key management.
type APIKeysConfig struct {
	// MaxExpirationDays is the maximum allowed expiration in days for API keys.
	// When set, users cannot create API keys with expiration longer than this value.
	// Examples: 30 (one month), 90 (three months), 365 (one year).
	// If not set, no expiration limit is enforced.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=1
	MaxExpirationDays *int32 `json:"maxExpirationDays,omitempty"`
}

// GatewayRef defines the reference to the global Gateway (Gw API) where
// models should be published to when exposed as services.
type GatewayRef struct {
	// Namespace is the namespace where the Gateway resource is located.
	// +kubebuilder:default="openshift-ingress"
	// +kubebuilder:validation:Pattern="^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$"
	// +kubebuilder:validation:MaxLength=63
	Namespace string `json:"namespace,omitempty"`

	// Name is the name of the Gateway resource.
	// +kubebuilder:default="maas-default-gateway"
	// +kubebuilder:validation:Pattern="^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$"
	// +kubebuilder:validation:MaxLength=63
	Name string `json:"name,omitempty"`
}

// ModelsAsServiceStatus defines the observed state of ModelsAsService
type ModelsAsServiceStatus struct {
	common.Status `json:",inline"`
}

// +kubebuilder:object:root=true
// ModelsAsServiceList contains a list of ModelsAsService
type ModelsAsServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ModelsAsService `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ModelsAsService{}, &ModelsAsServiceList{})
}

func (c *ModelsAsService) GetStatus() *common.Status {
	return &c.Status.Status
}

func (c *ModelsAsService) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *ModelsAsService) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

// DSCModelsAsServiceSpec enables ModelsAsService integration
type DSCModelsAsServiceSpec struct {
	// +kubebuilder:validation:Enum=Managed;Removed
	// +kubebuilder:default=Removed
	ManagementState operatorv1.ManagementState `json:"managementState,omitempty"`
}

// DSCModelsAsServiceStatus contains the observed state of the ModelsAsService exposed in the DSC instance
type DSCModelsAsServiceStatus struct {
	common.ManagementSpec `json:",inline"`
}
