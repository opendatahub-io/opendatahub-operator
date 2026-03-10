package clusterhealth

import (
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GVKs for the ODH custom resources checked by the DSCI and DSC sections.
var (
	DSCInitializationGVK = schema.GroupVersionKind{
		Group:   "dscinitialization.opendatahub.io",
		Version: "v2",
		Kind:    "DSCInitialization",
	}
	DataScienceClusterGVK = schema.GroupVersionKind{
		Group:   "datasciencecluster.opendatahub.io",
		Version: "v2",
		Kind:    "DataScienceCluster",
	}
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
	// SectionsRun is the list of section names that were executed (e.g. when using OnlySections or Layers).
	// Empty or nil means all sections were run. Used by PrettyPrint to show only those rows.
	SectionsRun []string `json:"sectionsRun,omitempty"`

	Nodes       SectionResult[NodesSection]        `json:"nodes"`
	Deployments SectionResult[DeploymentsSection]  `json:"deployments"`
	Pods        SectionResult[PodsSection]         `json:"pods"`
	Events      SectionResult[EventsSection]       `json:"events"`
	Quotas      SectionResult[QuotasSection]       `json:"quotas"`
	Operator    SectionResult[OperatorSection]     `json:"operator"`
	DSCI        SectionResult[CRConditionsSection] `json:"dsci"`
	DSC         SectionResult[CRConditionsSection] `json:"dsc"`
}

type NodesSection struct {
	Nodes []NodeInfo    `json:"nodes"`
	Data  []corev1.Node `json:"data,omitempty"` // raw Node list (e.g. .Status for NodeStatus) for tests or fields we don't parse
}

type NodeInfo struct {
	Name            string             `json:"name"`
	Conditions      []ConditionSummary `json:"conditions"`
	Allocatable     string             `json:"allocatable"` // human-readable (e.g. "4 CPU, 8Gi memory")
	Capacity        string             `json:"capacity"`
	UnhealthyReason string             `json:"unhealthyReason,omitempty"` // non-empty if node is in a bad state
}

type ConditionSummary struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type DeploymentsSection struct {
	ByNamespace map[string][]DeploymentInfo `json:"byNamespace"`
	Data        []appsv1.Deployment         `json:"data,omitempty"` // raw Deployment list for tests or fields we don't parse
}

type DeploymentInfo struct {
	Namespace  string             `json:"namespace"`
	Name       string             `json:"name"`
	Ready      int32              `json:"ready"`
	Replicas   int32              `json:"replicas"`
	Conditions []ConditionSummary `json:"conditions"`
}

type PodsSection struct {
	ByNamespace map[string][]PodInfo `json:"byNamespace"`
	Data        []corev1.Pod         `json:"data,omitempty"` // raw Pod list for tests or fields we don't parse
}

type PodInfo struct {
	Namespace  string          `json:"namespace"`
	Name       string          `json:"name"`
	Phase      string          `json:"phase"`
	Containers []ContainerInfo `json:"containers"`
}

type ContainerInfo struct {
	Name         string `json:"name"`
	Ready        bool   `json:"ready"`
	RestartCount int32  `json:"restartCount"`
	Waiting      string `json:"waiting"`    // reason/message if waiting
	Terminated   string `json:"terminated"` // reason/exit if terminated
}

type EventsSection struct {
	Events []EventInfo    `json:"events"`
	Data   []corev1.Event `json:"data,omitempty"` // raw Event list for tests or fields we don't parse
}

type EventInfo struct {
	Namespace string    `json:"namespace"`
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Reason    string    `json:"reason"`
	Message   string    `json:"message"`
	LastTime  time.Time `json:"lastTime"`
}

type QuotasSection struct {
	ByNamespace map[string][]ResourceQuotaInfo `json:"byNamespace"`
	Data        []corev1.ResourceQuota         `json:"data,omitempty"` // raw ResourceQuota list for tests or fields we don't parse
}

type ResourceQuotaInfo struct {
	Namespace string            `json:"namespace"`
	Name      string            `json:"name"`
	Used      map[string]string `json:"used"`
	Hard      map[string]string `json:"hard"`
	Exceeded  []string          `json:"exceeded"`
}

type OperatorSection struct {
	Deployment         *DeploymentInfo           `json:"deployment"`
	Pods               []PodInfo                 `json:"pods"`
	DependentOperators []DependentOperatorResult `json:"dependentOperators,omitempty"`
	Data               *OperatorSectionData      `json:"data,omitempty"` // raw Deployment and Pods for tests or fields we don't parse
}

type DependentOperatorResult struct {
	Name       string          `json:"name"`
	Installed  bool            `json:"installed"` // true if a deployment was found in the dependent's namespace
	Deployment *DeploymentInfo `json:"deployment,omitempty"`
	Pods       []PodInfo       `json:"pods,omitempty"`
	Error      string          `json:"error,omitempty"`
}

type OperatorSectionData struct {
	Deployment *appsv1.Deployment `json:"deployment,omitempty"`
	Pods       []corev1.Pod       `json:"pods,omitempty"`
}

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
