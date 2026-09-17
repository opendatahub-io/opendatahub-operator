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

import "github.com/opendatahub-io/opendatahub-operator/v2/api/common"

const (
	DashboardComponentName = "dashboard"
	// DashboardInstanceName the name of the Dashboard instance singleton.
	DashboardInstanceName = "default-" + DashboardComponentName
	DashboardKind         = "Dashboard"
)

// DashboardCommonSpec defines the v3 Dashboard configuration exposed in DSC.
type DashboardCommonSpec struct {
	// Standard controls the core Dashboard.
	Standard DashboardStandardSpec `json:"standard,omitempty"`
	// MaaSPortal controls the MaaS Consumer Portal independently of the core Dashboard.
	// +kubebuilder:default={managementState: "Removed"}
	MaaSPortal DashboardMaaSPortalSpec `json:"maasPortal,omitempty"`
}

// DashboardCommonSpecV2 defines the legacy v2 Dashboard configuration.
type DashboardCommonSpecV2 struct {
	// MaaSConsumerPortal controls the MaaS Consumer Portal submodule, shipped in
	// the dashboard-operator. It is managed independently of the core Dashboard.
	// +kubebuilder:default={managementState: "Removed"}
	MaaSConsumerPortal MaaSConsumerPortalSpec `json:"maasConsumerPortal,omitempty"`
}

// DashboardStandardSpec configures the core Dashboard lifecycle.
type DashboardStandardSpec struct {
	common.ManagementSpec `json:",inline"`
}

// DashboardMaaSPortalSpec configures the MaaS Consumer Portal lifecycle.
type DashboardMaaSPortalSpec struct {
	common.ManagementSpec `json:",inline"`
}

// MaaSConsumerPortalSpec configures the MaaS Consumer Portal submodule lifecycle.
type MaaSConsumerPortalSpec struct {
	common.ManagementSpec `json:",inline"`
}

// DashboardCommonStatus defines the shared observed state of Dashboard
type DashboardCommonStatus struct {
	URL string `json:"url,omitempty"`
}

// DSCDashboard contains the v3 Dashboard configuration exposed in a DSC.
type DSCDashboard struct {
	DashboardCommonSpec `json:",inline"`
}

// DSCDashboardV2 contains the legacy v2 Dashboard configuration exposed in a DSC.
type DSCDashboardV2 struct {
	common.ManagementSpec `json:",inline"`
	DashboardCommonSpecV2 `json:",inline"`
}

// DSCDashboardStatus contains the observed state of the Dashboard exposed in the DSC instance
type DSCDashboardStatus struct {
	common.ManagementSpec  `json:",inline"`
	*DashboardCommonStatus `json:",inline"`
}

// DSCMaaSConsumerPortalStatus contains the observed state of the MaaS Consumer
// Portal submodule (submodule of Dashboard) exposed in the DSC instance.
type DSCMaaSConsumerPortalStatus struct {
	common.ManagementSpec `json:",inline"`
}
