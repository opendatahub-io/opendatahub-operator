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
)

const (
	// This component is removed in 3.0
	CodeFlareKind = "CodeFlare"
)

// CodeFlareCommonStatus defines the shared observed state of CodeFlare
type CodeFlareCommonStatus struct {
	common.ComponentReleaseStatus `json:",inline"`
}

type DSCCodeFlare struct {
	common.ManagementSpec `json:",inline"`
}

// DSCCodeFlareStatus contains the observed state of the CodeFlare exposed in the DSC instance
type DSCCodeFlareStatus struct {
	common.ManagementSpec  `json:",inline"`
	*CodeFlareCommonStatus `json:",inline"`
}
