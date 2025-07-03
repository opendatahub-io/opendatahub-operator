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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// SchedulingType defines the scheduling method for the hardware profile.
type SchedulingType string

const (
	// QueueScheduling indicates that workloads should be scheduled through a queue.
	QueueScheduling SchedulingType = "Queue"

	// NodeScheduling indicates that workloads should be scheduled directly to nodes.
	NodeScheduling SchedulingType = "Node"
)

// HardwareProfileSpec defines the desired state of HardwareProfile.
type HardwareProfileSpec struct {
	// The array of identifiers
	// +optional
	Identifiers []HardwareIdentifier `json:"identifiers,omitempty"`

	// SchedulingSpec specifies how workloads using this hardware profile should be scheduled.
	// +optional
	SchedulingSpec *SchedulingSpec `json:"scheduling,omitempty"`
}

type HardwareIdentifier struct {
	// The display name of identifier.
	DisplayName string `json:"displayName"`

	// The resource identifier of the hardware device.
	Identifier string `json:"identifier"`

	// The minimum count can be an integer or a string.
	MinCount intstr.IntOrString `json:"minCount"`

	// The maximum count can be an integer or a string.
	// +optional
	MaxCount *intstr.IntOrString `json:"maxCount,omitempty"`

	// The default count can be an integer or a string.
	DefaultCount intstr.IntOrString `json:"defaultCount"`

	// The type of identifier. could be "CPU", "Memory", or "Accelerator". Leave it undefined for the other types.
	// +optional
	// +kubebuilder:validation:Enum=CPU;Memory;Accelerator
	ResourceType string `json:"resourceType,omitempty"`
}

// SchedulingSpec allows for specifying either kueue-based scheduling or direct node scheduling.
// CEL Rule 1: If schedulingType is "Queue", the 'kueue' field (with a non-empty localQueueName) must be set, and the 'node' field must not be set.
// +kubebuilder:validation:XValidation:rule="self.type == 'Queue' ? (has(self.kueue) && has(self.kueue.localQueueName) && !has(self.node)) : true",message="When schedulingType is 'Queue', the 'kueue.localQueueName' field must be specified and non-empty, and the 'node' field must not be set"
// CEL Rule 2: If schedulingType is "Node", the 'node' field must be set, and the 'kueue' field must not be set.
// +kubebuilder:validation:XValidation:rule="self.type == 'Node' ? (has(self.node) && !has(self.kueue)) : true",message="When schedulingType is 'Node', the 'node' field must be set, and the 'kueue' field must not be set"
type SchedulingSpec struct {
	// SchedulingType is the scheduling method discriminator.
	// Users must set this value to indicate which scheduling method to use.
	// The value of this field should match exactly one configured scheduling method.
	// Valid values are "Queue" and "Node".
	// +kubebuilder:validation:Enum="Queue";"Node"
	// +kubebuilder:validation:Required
	SchedulingType SchedulingType `json:"type"`

	// Kueue specifies queue-based scheduling configuration.
	// This field is only valid when schedulingType is "Queue".
	// +optional
	Kueue *KueueSchedulingSpec `json:"kueue,omitempty"`

	// node specifies direct node scheduling configuration.
	// This field is only valid when schedulingType is "Node".
	// +optional
	Node *NodeSchedulingSpec `json:"node,omitempty"`
}

// KueueSchedulingSpec defines queue-based scheduling configuration.
type KueueSchedulingSpec struct {
	// LocalQueueName specifies the name of the local queue to use for workload scheduling.
	// When specified, workloads using this hardware profile will be submitted to the
	// specified queue and the queue's configuration will determine the actual node
	// placement and tolerations.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	LocalQueueName string `json:"localQueueName"`

	// PriorityClass specifies the name of the WorkloadPriorityClass associated with the HardwareProfile.
	// +optional
	PriorityClass string `json:"priorityClass,omitempty"`
}

// NodeSchedulingSpec defines direct node scheduling configuration.
type NodeSchedulingSpec struct {
	// NodeSelector specifies the node selector to use for direct node scheduling.
	// Workloads will be scheduled only on nodes that match all the specified labels.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations specifies the tolerations to apply to workloads for direct node scheduling.
	// These tolerations allow workloads to be scheduled on nodes with matching taints.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
}

// HardwareProfileStatus defines the observed state of HardwareProfile.
type HardwareProfileStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
//+kubebuilder:subresource:status

// HardwareProfile is the Schema for the hardwareprofiles API.
type HardwareProfile struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HardwareProfileSpec   `json:"spec"`
	Status HardwareProfileStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// HardwareProfileList contains a list of HardwareProfile.
type HardwareProfileList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HardwareProfile `json:"items"`
}

func init() {
	SchemeBuilder.Register(&HardwareProfile{}, &HardwareProfileList{})
}

// +kubebuilder:object:root=true

// DashboardHardwareProfile represents the dashboard.opendatahub.io HardwareProfile CRD. This will be a temporary struct until the migration is complete
type DashboardHardwareProfile struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              DashboardHardwareProfileSpec `json:"spec"`
}

type DashboardHardwareProfileSpec struct {
	DisplayName  string               `json:"displayName"`
	Enabled      bool                 `json:"enabled"`
	Description  string               `json:"description,omitempty"`
	Tolerations  []corev1.Toleration  `json:"tolerations,omitempty"`
	Identifiers  []HardwareIdentifier `json:"identifiers,omitempty"`
	NodeSelector map[string]string    `json:"nodeSelector,omitempty"`
}

// +kubebuilder:object:root=true

// DashboardHardwareProfileList contains a list of DashboardHardwareProfile.
type DashboardHardwareProfileList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DashboardHardwareProfile `json:"items"`
}
