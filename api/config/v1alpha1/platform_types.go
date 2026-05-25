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

import (
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

const (
	PlatformKind         = "Platform"
	PlatformInstanceName = "default"
)

var _ common.PlatformObject = (*Platform)(nil)

// PlatformSpec defines the desired state of Platform.
type PlatformSpec struct {
	// Modules declares the set of modules managed by this Platform instance.
	// Each field corresponds to a registered module handler. Modules follow
	// the same Managed/Removed/empty convention as DSC components: Managed
	// deploys the module, Removed tears it down, empty means not managed.
	// +optional
	Modules PlatformModules `json:"modules,omitempty"`
}

// PlatformModules declares per-module management state for Platform mode.
// Each field maps to a registered module handler by name. Add new module
// fields here when onboarding additional modules.
// +kubebuilder:object:generate=true
type PlatformModules struct {
	// Monitoring controls the monitoring module operator lifecycle.
	// +optional
	Monitoring common.ManagementSpec `json:"monitoring,omitempty"`
}

// PlatformStatus defines the observed state of Platform.
type PlatformStatus struct {
	common.Status `json:",inline"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:storageversion
// +kubebuilder:resource:scope=Cluster,shortName=odhp
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default'",message="Platform name must be default"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// Platform is the Schema for the platforms API. It serves as the primary
// reconcile trigger for the module reconciler on clusters where
// DataScienceCluster is not installed (xKS / vanilla Kubernetes).
type Platform struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PlatformSpec   `json:"spec,omitempty"`
	Status PlatformStatus `json:"status,omitempty"`
}

func (p *Platform) GetStatus() *common.Status {
	return &p.Status.Status
}

func (p *Platform) GetConditions() []common.Condition {
	return p.Status.GetConditions()
}

func (p *Platform) SetConditions(conditions []common.Condition) {
	p.Status.SetConditions(conditions)
}

//+kubebuilder:object:root=true

// PlatformList contains a list of Platform.
type PlatformList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Platform `json:"items"`
}

// EnabledModules returns the names of modules whose ManagementState is Managed.
func (m *PlatformModules) EnabledModules() []string {
	var enabled []string
	if m.Monitoring.ManagementState == operatorv1.Managed {
		enabled = append(enabled, "monitoring")
	}
	return enabled
}

func init() {
	SchemeBuilder.Register(&Platform{}, &PlatformList{})
}
