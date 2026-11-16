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

package v1alpha2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
)

const (
	PlatformModuleKind = "PlatformModule"
)

var _ common.PlatformObject = (*PlatformModule)(nil)

// ResourceRef identifies a Kubernetes resource deployed by a module operator.
// Used to track deployed resources for status reporting and cleanup.
type ResourceRef struct {
	// Group of the resource (empty for the core API group).
	// +optional
	Group string `json:"group,omitempty"`

	// Version of the resource.
	// +required
	Version string `json:"version"`

	// Kind of the resource (e.g. Deployment, Service, ClusterRole).
	// +required
	Kind string `json:"kind"`

	// Namespace of the resource (empty for cluster-scoped resources).
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Name of the resource.
	// +required
	Name string `json:"name"`
}

// GroupVersionKind returns the Kubernetes type identified by this reference.
func (r ResourceRef) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: r.Group, Version: r.Version, Kind: r.Kind}
}

// PlatformModuleSpec defines the desired state of PlatformModule.
// Empty: the presence of a PlatformModule CR indicates the module should be
// deployed. Desired configuration lives in Platform.Spec.Modules[name], while
// ordering comes from the unified provision registry.
type PlatformModuleSpec struct {
}

// PlatformModuleStatus defines the observed state of PlatformModule.
type PlatformModuleStatus struct {
	common.Status `json:",inline"`

	// Release identifies the platform version whose operator resources were
	// successfully applied and tracker-owned cleanup completed for this inventory.
	// +optional
	Release common.Release `json:"release,omitempty"`

	// Resources is desired operator-resource inventory from the last fully
	// successful apply and stale cleanup. It advances with Release and drives
	// finalizer cleanup. Runtime readiness is reported separately in conditions.
	// CRDs, Namespaces, and Leases can appear in inventory but are never deleted.
	// +optional
	Resources []ResourceRef `json:"resources,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:storageversion
// +kubebuilder:resource:scope=Cluster,shortName=odhpm
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"

// PlatformModule tracks the deployment lifecycle of a single module's
// operator. It is a pure status tracker: the desired state (whether the
// module is enabled) lives on the Platform CR, and this CR's presence on the
// cluster is the deploy signal for the PlatformModule controller.
type PlatformModule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PlatformModuleSpec   `json:"spec,omitempty"`
	Status PlatformModuleStatus `json:"status,omitempty"`
}

func (pm *PlatformModule) GetStatus() *common.Status {
	return &pm.Status.Status
}

func (pm *PlatformModule) GetConditions() []common.Condition {
	return pm.Status.GetConditions()
}

func (pm *PlatformModule) SetConditions(conditions []common.Condition) {
	pm.Status.SetConditions(conditions)
}

//+kubebuilder:object:root=true

// PlatformModuleList contains a list of PlatformModule.
type PlatformModuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PlatformModule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PlatformModule{}, &PlatformModuleList{})
}
