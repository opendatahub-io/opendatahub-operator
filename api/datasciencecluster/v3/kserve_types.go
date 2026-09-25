package v3

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
)

// KserveCommonSpec spec defines the shared desired state of Kserve
type KserveCommonSpec struct {
	// Configures the type of service that is created for InferenceServices using RawDeployment.
	// The values for RawDeploymentServiceConfig can be "Headless" (default value) or "Headed".
	// Headless: to set "ServiceClusterIPNone = true" in the 'inferenceservice-config' configmap for Kserve.
	// Headed: to set "ServiceClusterIPNone = false" in the 'inferenceservice-config' configmap for Kserve.
	// +kubebuilder:default=Headless
	RawDeploymentServiceConfig componentApi.RawServiceConfig `json:"rawDeploymentServiceConfig,omitempty"`

	// Configures the OAuth proxy sidecar container resources in the
	// 'inferenceservice-config' ConfigMap for KServe. Only non-nil fields
	// override the defaults shipped with the operator manifests.
	// +optional
	OAuthProxy *componentApi.OAuthProxyConfig `json:"oauthProxy,omitempty"`

	// Configures and enables NVIDIA NIM integration
	// +kubebuilder:default={}
	NIM componentApi.NimSpec `json:"nim,omitempty"`

	// Configures and enables workload-variant-autoscaler (WVA) integration
	// +kubebuilder:default={}
	WVA componentApi.WVASpec `json:"wva,omitempty"`

	// Enables TLS for LLMInferenceService deployments.
	// When unset, the KServe default (TLS enabled) is preserved.
	// +optional
	EnableLLMInferenceServiceTLS *bool `json:"enableLLMInferenceServiceTLS,omitempty"`

	// Enables OpenShift Developer Console dashboards for LLMInferenceService.
	// Enabled by default.
	// +optional
	EnableLLMInferenceServiceConsoleDashboards *bool `json:"enableLLMInferenceServiceConsoleDashboards,omitempty"`

	// Configures and enables Model Cache integration
	ModelCache *componentApi.ModelCacheSpec `json:"modelCache,omitempty"`
}

// DSCKserve contains the v3 KServe configuration. MaaS belongs to AI Gateway.
type DSCKserve struct {
	common.ManagementSpec `json:",inline"`
	KserveCommonSpec      `json:",inline"`
}
