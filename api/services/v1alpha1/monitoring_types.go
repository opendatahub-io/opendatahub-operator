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
	resource "k8s.io/apimachinery/pkg/api/resource"
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
	// monitoring spec exposed only to internal api
}

// Metrics defines the desired state of metrics for the monitoring service
// +kubebuilder:validation:XValidation:rule="!(self.storage == null && self.resources == null) || !has(self.replicas) || self.replicas == 0",message="Replicas can only be set to non-zero value when either Storage or Resources is configured"
type Metrics struct {
	Storage   *MetricsStorage   `json:"storage,omitempty"`
	Resources *MetricsResources `json:"resources,omitempty"`
	// Replicas specifies the number of replicas in monitoringstack, default is 2 if not set
	Replicas int32 `json:"replicas,omitempty"`
	// Exporters defines custom metrics exporters for sending metrics to external observability tools.
	// Each key-value pair represents an exporter name and its configuration.
	// Reserved names 'prometheus' and 'otlp/tempo' cannot be used as they conflict with built-in exporters.
	// +optional
	// +kubebuilder:validation:XValidation:rule="!('prometheus' in self)",message="exporter name 'prometheus' is reserved and cannot be used"
	// +kubebuilder:validation:XValidation:rule="!('otlp/tempo' in self)",message="exporter name 'otlp/tempo' is reserved and cannot be used"
	// +kubebuilder:validation:XValidation:rule="self.all(k, self[k] != '')",message="exporter configuration values must be non-empty strings"
	Exporters map[string]string `json:"exporters,omitempty"`
}

// MetricsStorage defines the storage configuration for the monitoring service
type MetricsStorage struct {
	// Size specifies the storage size for the MonitoringStack (e.g, "5Gi", "10Mi")
	// +kubebuilder:default="5Gi"
	Size resource.Quantity `json:"size,omitempty"`
	// Retention specifies how long metrics data should be retained (e.g., "1d", "2w")
	// +kubebuilder:default="90d"
	Retention string `json:"retention,omitempty"`
}

// MetricsResources defines the resource requests and limits for the monitoring service
type MetricsResources struct {
	// CPULimit specifies the maximum CPU allocation (e.g., "500m", "2")
	// +kubebuilder:default="500m"
	CPULimit resource.Quantity `json:"cpulimit,omitempty"`
	// MemoryLimit specifies the maximum memory allocation (e.g., "1Gi", "512Mi")
	// +kubebuilder:default="512Mi"
	MemoryLimit resource.Quantity `json:"memorylimit,omitempty"`
	// CPURequest specifies the minimum CPU allocation (e.g., "100m", "0.5")
	// +kubebuilder:default="100m"
	CPURequest resource.Quantity `json:"cpurequest,omitempty"`
	// MemoryRequest specifies the minimum memory allocation (e.g., "256Mi", "1Gi")
	// +kubebuilder:default="256Mi"
	MemoryRequest resource.Quantity `json:"memoryrequest,omitempty"`
}

// MonitoringStatus defines the observed state of Monitoring
type MonitoringStatus struct {
	common.Status `json:",inline"`

	URL string `json:"url,omitempty"`
}

// Traces enables and defines the configuration for traces collection
type Traces struct {
	Storage TracesStorage `json:"storage"`
	// SampleRatio determines the sampling rate for traces
	// Value should be between 0.0 (no sampling) and 1.0 (sample all traces)
	// +kubebuilder:default="0.1"
	// +kubebuilder:validation:Pattern="^(0(\\.[0-9]+)?|1(\\.0+)?)$"
	SampleRatio string `json:"sampleRatio,omitempty"`
}

// TracesStorage defines the storage configuration for tracing.
// +kubebuilder:validation:XValidation:rule="self.backend != 'pv' ? has(self.secret) : true", message="When backend is s3 or gcs, the 'secret' field must be specified and non-empty"
// +kubebuilder:validation:XValidation:rule="self.backend != 'pv' ? !has(self.size) : true", message="Size is supported when backend is pv only"
type TracesStorage struct {
	// Backend defines the storage backend type.
	// Valid values are "pv", "s3", and "gcs".
	// +kubebuilder:validation:Enum="pv";"s3";"gcs"
	// +kubebuilder:default:="pv"
	Backend string `json:"backend"`

	// Size specifies the size of the storage.
	// This field is optional.
	// +optional
	Size string `json:"size,omitempty"`

	// Secret specifies the secret name for storage credentials.
	// This field is required when the backend is not "pv".
	// +optional
	Secret string `json:"secret,omitempty"`

	// Retention specifies how long trace data should be retained globally (e.g., "60m", "10h")
	// +kubebuilder:default="2160h"
	Retention metav1.Duration `json:"retention,omitempty"`
}

// Alerting configuration for Prometheus
type Alerting struct {
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
// +kubebuilder:validation:XValidation:rule="has(self.alerting) ? has(self.metrics.storage) || has(self.metrics.resources) : true",message="Alerting configuration requires metrics.storage or metrics.resources to be configured"
type MonitoringCommonSpec struct {
	// monitoring spec exposed to DSCI api
	// Namespace for monitoring if it is enabled
	// +kubebuilder:default=redhat-ods-monitoring
	// +kubebuilder:validation:Pattern="^([a-z0-9]([-a-z0-9]*[a-z0-9])?)?$"
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="MonitoringNamespace is immutable"
	Namespace string `json:"namespace,omitempty"`
	// metrics collection
	Metrics *Metrics `json:"metrics,omitempty"`
	// Tracing configuration for OpenTelemetry instrumentation
	Traces *Traces `json:"traces,omitempty"`
	// Alerting configuration for Prometheus
	Alerting *Alerting `json:"alerting,omitempty"`
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
