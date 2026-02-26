package clusterhealth

import (
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// SectionResult carries the result of one health-check section: optional error and typed data.
// Partial failures set Error and may still populate Data with what was collected.
type SectionResult[T any] struct {
	Error string `json:"error"` // non-empty if this section failed or was skipped
	Data  T      `json:"data"`  // populated with whatever was collected (may be zero value)
}

// Report is the full result of Run(). All sections are independent; partial failures
// are recorded per section.
type Report struct {
	CollectedAt time.Time `json:"collectedAt"`

	Nodes       SectionResult[NodesSection]        `json:"nodes"`
	Deployments SectionResult[DeploymentsSection]  `json:"deployments"`
	Pods        SectionResult[PodsSection]         `json:"pods"`
	Events      SectionResult[EventsSection]       `json:"events"`
	Quotas      SectionResult[QuotasSection]       `json:"quotas"`
	Operator    SectionResult[OperatorSection]     `json:"operator"`
	DSCI        SectionResult[CRConditionsSection] `json:"dsci"`
	DSC         SectionResult[CRConditionsSection] `json:"dsc"`
}

// NodesSection: node conditions and resource allocation.
type NodesSection struct {
	Nodes []NodeInfo    `json:"nodes"`
	Data  []corev1.Node `json:"data,omitempty"` // raw Node list (e.g. .Status for NodeStatus) for tests or fields we don't parse
}

// NodeInfo holds summary info for one node.
type NodeInfo struct {
	Name            string             `json:"name"`
	Conditions      []ConditionSummary `json:"conditions"`
	Allocatable     string             `json:"allocatable"` // human-readable (e.g. "4 CPU, 8Gi memory")
	Capacity        string             `json:"capacity"`
	UnhealthyReason string             `json:"unhealthyReason,omitempty"` // non-empty if node is in a bad state
}

// ConditionSummary is a minimal condition for reporting.
type ConditionSummary struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// DeploymentsSection: deployment readiness per namespace.
type DeploymentsSection struct {
	ByNamespace map[string][]DeploymentInfo `json:"byNamespace"`
	Data        []appsv1.Deployment         `json:"data,omitempty"` // raw Deployment list for tests or fields we don't parse
}

// DeploymentInfo holds readiness and conditions for one deployment.
type DeploymentInfo struct {
	Namespace  string             `json:"namespace"`
	Name       string             `json:"name"`
	Ready      int32              `json:"ready"`
	Replicas   int32              `json:"replicas"`
	Conditions []ConditionSummary `json:"conditions"`
}

// PodsSection: pod phases and container states.
type PodsSection struct {
	ByNamespace map[string][]PodInfo `json:"byNamespace"`
	Data        []corev1.Pod         `json:"data,omitempty"` // raw Pod list for tests or fields we don't parse
}

// PodInfo holds phase and container summary for one pod.
type PodInfo struct {
	Namespace  string          `json:"namespace"`
	Name       string          `json:"name"`
	Phase      string          `json:"phase"`
	Containers []ContainerInfo `json:"containers"`
}

// ContainerInfo holds ready/restart and state for one container.
type ContainerInfo struct {
	Name         string `json:"name"`
	Ready        bool   `json:"ready"`
	RestartCount int32  `json:"restartCount"`
	Waiting      string `json:"waiting"`    // reason/message if waiting
	Terminated   string `json:"terminated"` // reason/exit if terminated
}

// EventsSection: recent (e.g. warning) events.
type EventsSection struct {
	Events []EventInfo    `json:"events"`
	Data   []corev1.Event `json:"data,omitempty"` // raw Event list for tests or fields we don't parse
}

// EventInfo holds one event for reporting.
type EventInfo struct {
	Namespace string    `json:"namespace"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Reason    string    `json:"reason"`
	Message   string    `json:"message"`
	LastTime  time.Time `json:"lastTime"`
}

// QuotasSection: resource quota usage per namespace.
type QuotasSection struct {
	ByNamespace map[string][]ResourceQuotaInfo `json:"byNamespace"`
	Data        []corev1.ResourceQuota         `json:"data,omitempty"` // raw ResourceQuota list for tests or fields we don't parse
}

// ResourceQuotaInfo holds used/hard and exceeded resources for one quota.
type ResourceQuotaInfo struct {
	Namespace string            `json:"namespace"`
	Name      string            `json:"name"`
	Used      map[string]string `json:"used"`
	Hard      map[string]string `json:"hard"`
	Exceeded  []string          `json:"exceeded"`
}

// OperatorSection: operator deployment and pod status.
type OperatorSection struct {
	Deployment *DeploymentInfo      `json:"deployment"`
	Pods       []PodInfo            `json:"pods"`
	Data       *OperatorSectionData `json:"data,omitempty"` // raw Deployment and Pods for tests or fields we don't parse
}

// OperatorSectionData holds raw Kubernetes objects for the operator section.
type OperatorSectionData struct {
	Deployment *appsv1.Deployment `json:"deployment,omitempty"`
	Pods       []corev1.Pod       `json:"pods,omitempty"`
}

// CRConditionsSection: conditions from a CR (DSCI or DSC).
type CRConditionsSection struct {
	Name       string                     `json:"name"`
	Conditions []ConditionSummary         `json:"conditions"`
	Data       *unstructured.Unstructured `json:"data,omitempty"` // raw CR for tests or fields we don't parse
}

// Healthy returns true if the report has no section errors (all checks succeeded or returned partial data without a fatal error).
// Used by CLI to decide exit code.
func (r *Report) Healthy() bool {
	return r.Nodes.Error == "" &&
		r.Deployments.Error == "" &&
		r.Pods.Error == "" &&
		r.Events.Error == "" &&
		r.Quotas.Error == "" &&
		r.Operator.Error == "" &&
		r.DSCI.Error == "" &&
		r.DSC.Error == ""
}
