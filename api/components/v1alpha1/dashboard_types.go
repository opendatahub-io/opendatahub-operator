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
)

const (
	DashboardComponentName = "dashboard"
	// DashboardInstanceName the name of the Dashboard instance singleton.
	DashboardInstanceName = "default-" + DashboardComponentName
	DashboardKind         = "Dashboard"
)

// DashboardCommonSpec spec defines the shared desired state of Dashboard (used in DSC and Dashboard CR).
type DashboardCommonSpec struct {
	// dashboard spec exposed to DSC api
	// dashboard spec exposed only to internal api
}

// DashboardCommonStatus defines the shared observed state of Dashboard
type DashboardCommonStatus struct {
	URL string `json:"url,omitempty"`
}

// DSCDashboard contains all the configuration exposed in DSC instance for Dashboard component
type DSCDashboard struct {
	// configuration fields common across components
	common.ManagementSpec `json:",inline"`
	// dashboard specific field
	DashboardCommonSpec `json:",inline"`
}

// DSCDashboardStatus contains the observed state of the Dashboard exposed in the DSC instance
type DSCDashboardStatus struct {
	common.ManagementSpec  `json:",inline"`
	*DashboardCommonStatus `json:",inline"`
}
