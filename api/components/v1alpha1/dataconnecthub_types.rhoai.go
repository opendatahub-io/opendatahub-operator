//go:build rhoai

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

// DefaultDataConnectHubNamespace contains shared DCH application infrastructure.
const DefaultDataConnectHubNamespace = "redhat-dch-infra"

// DataConnectHubCommonSpec defines the public DCH namespace configuration.
type DataConnectHubCommonSpec struct {
	// Namespace contains the DCH services, not the module/controller.
	// Defaults to "redhat-dch-infra" when omitted, or when empty and Managed.
	// A non-empty namespace cannot change while DCH remains Managed;
	// set Removed before changing it.
	// +kubebuilder:default="redhat-dch-infra"
	// +kubebuilder:validation:Pattern="^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$"
	// +kubebuilder:validation:MaxLength=63
	Namespace string `json:"namespace,omitempty"`
}
