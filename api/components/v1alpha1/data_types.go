/*
Copyright 2026.

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

import "github.com/opendatahub-io/opendatahub-operator/v2/api/common"

const (
	DataModuleName            = "data"
	DataRegistryComponentName = "dataRegistry"
)

// DSCFeatureStore defines the Feature Store configuration exposed in a DSC.
type DSCFeatureStore struct {
	common.ManagementSpec `json:",inline"`
}

// DSCDataRegistry defines the Data Registry configuration exposed in a DSC.
type DSCDataRegistry struct {
	common.ManagementSpec `json:",inline"`
}

// DataConnectHubGateway identifies the platform Gateway used by DCH.
type DataConnectHubGateway struct {
	// Name is the Gateway resource name.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	// +kubebuilder:validation:XValidation:rule="self.trim() != ''",message="Gateway name must not be blank"
	Name string `json:"name"`

	// Namespace contains the Gateway resource.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:XValidation:rule="self.trim() != ''",message="Gateway namespace must not be blank"
	Namespace string `json:"namespace"`
}

// DSCDataConnectHub configures DCH within the shared Data module.
type DSCDataConnectHub struct {
	common.ManagementSpec `json:",inline"`

	// Namespace contains the DCH services, not the module/controller.
	// Defaults to odh-data-connect-hub-infra on ODH and
	// rhoai-data-connect-hub-infra on RHOAI when Managed.
	// +kubebuilder:validation:Pattern="^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$"
	// +kubebuilder:validation:MaxLength=63
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Gateway overrides the platform-discovered Gateway.
	// +optional
	Gateway *DataConnectHubGateway `json:"gateway,omitempty"`

	// TokenReviewAudiences overrides platform-discovered service-account audiences.
	// Omitted or empty means use platform discovery.
	// +kubebuilder:validation:MaxItems=32
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=256
	// +kubebuilder:validation:XValidation:rule="self.all(audience, audience.trim() != '')",message="TokenReview audiences must not be blank"
	// +listType=set
	// +optional
	TokenReviewAudiences []string `json:"tokenReviewAudiences,omitempty"`
}

// DSCData defines the independent data-related component configurations
// exposed in the v3 DSC.
type DSCData struct {
	FeatureStore DSCFeatureStore `json:"featureStore,omitempty"`
	DataRegistry DSCDataRegistry `json:"dataRegistry,omitempty"`
	// DataConnectHub is installed by the existing Data module operator. An
	// omitted managementState is treated as Removed.
	DataConnectHub DSCDataConnectHub `json:"dataConnectHub,omitempty"`
}

// DataCommonStatus defines the observed state shared by the Data module.
type DataCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

// DSCDataStatus contains the public v3 Data status exposed in the DSC.
type DSCDataStatus struct {
	common.ManagementSpec `json:",inline"`
	*DataCommonStatus     `json:",inline"`
}
