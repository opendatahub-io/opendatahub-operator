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

// DSCFeatureStore defines the Feature Store configuration exposed in a DSC.
type DSCFeatureStore struct {
	common.ManagementSpec `json:",inline"`
}

// DSCDataRegistry defines the Data Registry configuration exposed in a DSC.
type DSCDataRegistry struct {
	common.ManagementSpec `json:",inline"`
}

// DSCData defines the independent data-related component configurations
// exposed in the v3 DSC.
type DSCData struct {
	FeatureStore DSCFeatureStore `json:"featureStore,omitempty"`
	DataRegistry DSCDataRegistry `json:"dataRegistry,omitempty"`
}
