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
	DataModuleName             = "data"
	DataRegistryComponentName  = "dataRegistry"
	DataConnectHubModuleName   = "dataconnecthub"
	DataConnectHubKind         = "DataConnectHub"
	DataConnectHubInstanceName = "default-dataconnecthub"
)

// DSCFeatureStore defines the Feature Store configuration exposed in a DSC.
type DSCFeatureStore struct {
	common.ManagementSpec `json:",inline"`
}

// DSCDataRegistry defines the Data Registry configuration exposed in a DSC.
type DSCDataRegistry struct {
	common.ManagementSpec `json:",inline"`
}

// DataConnectHubGateway identifies a Gateway resource for Data Connect Hub.
type DataConnectHubGateway struct {
	// name is the Gateway resource name.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`

	// namespace is the namespace containing the Gateway resource.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	Namespace string `json:"namespace"`
}

// DSCDataConnectHub defines the Data Connect Hub configuration exposed in a DSC.
type DSCDataConnectHub struct {
	common.ManagementSpec `json:",inline"`

	// gateway overrides the platform-discovered Gateway. When omitted, the
	// platform default Gateway is used.
	// +optional
	Gateway *DataConnectHubGateway `json:"gateway,omitempty"`

	// tokenReviewAudiences overrides the platform-discovered Kubernetes
	// TokenReview audiences. When omitted or empty, platform discovery applies.
	// +kubebuilder:validation:MaxItems=32
	// +listType=set
	// +kubebuilder:validation:items:MaxLength=256
	// +kubebuilder:validation:items:MinLength=1
	// +optional
	TokenReviewAudiences []string `json:"tokenReviewAudiences,omitempty"`
}

// DSCData defines the independent data-related component configurations
// exposed in the v3 DSC.
type DSCData struct {
	FeatureStore DSCFeatureStore `json:"featureStore,omitempty"`
	DataRegistry DSCDataRegistry `json:"dataRegistry,omitempty"`
	// DataConnectHub controls Data Connect Hub independently of Feature Store and
	// Data Registry. An omitted managementState is treated as Removed.
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
