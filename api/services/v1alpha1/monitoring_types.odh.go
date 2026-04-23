//go:build !rhoai

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

// MonitoringCommonSpec spec defines the shared desired state of Monitoring
// +kubebuilder:validation:XValidation:rule="has(self.alerting) ? has(self.metrics.storage)  : true",message="Alerting configuration requires metrics.storage to be configured"
// +kubebuilder:validation:XValidation:rule="!has(self.collectorReplicas) || (self.collectorReplicas > 0 && (self.metrics.storage != null || self.traces != null))",message="CollectorReplicas can only be set when metrics.storage or traces are configured, and must be > 0"
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
	// Tracing configuration for OpenTelemetry instrumentation
	Traces *Traces `json:"traces,omitempty"`
	// Alerting configuration for Prometheus
	Alerting *Alerting `json:"alerting,omitempty"`
	// CollectorReplicas specifies the number of replicas in opentelemetry-collector. If not set, it defaults
	// to 1 on single-node clusters and 2 on multi-node clusters.
	CollectorReplicas int32 `json:"collectorReplicas,omitempty"`
}
