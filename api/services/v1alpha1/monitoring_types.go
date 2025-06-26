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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	MonitoringServiceName = "monitoring"
	// MonitoringInstanceName the name of the Monitoring instance singleton.
	// value should match whats set in the XValidation below
	MonitoringInstanceName = "default-monitoring"
	MonitoringKind         = "Monitoring"
)

// Check that the component implements common.PlatformObject.
var _ common.PlatformObject = (*Monitoring)(nil)

// MonitoringSpec defines the desired state of Monitoring
type MonitoringSpec struct {
	// monitoring spec exposed to DSCI api
	MonitoringCommonSpec `json:",inline"`
	// metrics collection
	*Metrics `json:"metrics,omitempty"`
	// traces collection
	*Traces `json:"traces,omitempty"`
	// monitoring spec exposed only to internal api
}

// Metrics defines the desired state of metrics for the monitoring service
type Metrics struct {
	Storage   MetricsStorage   `json:"storage,omitempty"`
	Resources MetricsResources `json:"resources,omitempty"`
}

// MetricsStorage defines the desired state of storage for the monitoring service
type MetricsStorage struct {
	// Size of the storage in Gi
	Size int `json:"size,omitempty"`
	// Retention of the storage in days
	Retention int `json:"retention,omitempty"`
}

// MetricsResources defines the desired state of resource requests and limits for the monitoring service
type MetricsResources struct {
	// CPU limit for the monitoring service
	CPULimit string `json:"cpulimit,omitempty"`
	// Memory limit for the monitoring service
	MemoryLimit string `json:"memorylimit,omitempty"`
	// CPU request for the monitoring service
	CPURequest string `json:"cpurequest,omitempty"`
	// Memory request for the monitoring service
	MemoryRequest string `json:"memoryrequest,omitempty"`
}

// Traces defines the desired state of traces for the monitoring service
type Traces struct{}

// MonitoringStatus defines the observed state of Monitoring
type MonitoringStatus struct {
	common.Status `json:",inline"`

	URL string `json:"url,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:validation:XValidation:rule="self.metadata.name == 'default-monitoring'",message="Monitoring name must be default-monitoring"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`,description="Ready"
// +kubebuilder:printcolumn:name="Reason",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].reason`,description="Reason"
// +kubebuilder:printcolumn:name="URL",type=string,JSONPath=`.status.url`,description="URL"

// Monitoring is the Schema for the monitorings API
type Monitoring struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MonitoringSpec   `json:"spec,omitempty"`
	Status MonitoringStatus `json:"status,omitempty"`
}

// MonitoringCommonSpec spec defines the shared desired state of Dashboard
type MonitoringCommonSpec struct {
	// monitoring spec exposed to DSCI api
	// Namespace for monitoring if it is enabled
	// +kubebuilder:default=opendatahub
	// +kubebuilder:validation:Pattern="^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$"
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="MonitoringNamespace is immutable"
	Namespace string `json:"namespace,omitempty"`
	// metrics collection
	Metrics *Metrics `json:"metrics,omitempty"`
	// traces collection
	Traces *Traces `json:"traces,omitempty"`
}

//+kubebuilder:object:root=true

// MonitoringList contains a list of Monitoring
type MonitoringList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Monitoring `json:"items"`
}

func (m *Monitoring) GetDevFlags() *common.DevFlags {
	return nil
}

func (m *Monitoring) GetStatus() *common.Status {
	return &m.Status.Status
}

func (c *Monitoring) GetConditions() []common.Condition {
	return c.Status.GetConditions()
}

func (c *Monitoring) SetConditions(conditions []common.Condition) {
	c.Status.SetConditions(conditions)
}

func init() {
	SchemeBuilder.Register(&Monitoring{}, &MonitoringList{})
}

type DSCIMonitoring struct {
	// configuration fields common across services
	common.ManagementSpec `json:",inline"`
	// monitoring specific fields
	MonitoringCommonSpec `json:",inline"`
}
