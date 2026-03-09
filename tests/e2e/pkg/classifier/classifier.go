package classifier

import (
	"fmt"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/clusterhealth"
)

// Classify inspects a clusterhealth Report and categorizes the failure.
// If report is nil, returns unknown/unclassifiable/3000/low.
func Classify(report *clusterhealth.Report) FailureClassification {
	if report == nil {
		return unknown()
	}

	// Per-section classifiers, ordered by priority (first match wins).
	sectionClassifiers := []func(*clusterhealth.Report) *FailureClassification{
		classifyFromPods,
		classifyFromEvents,
		classifyFromQuotas,
		classifyFromNodes,
	}

	for _, fn := range sectionClassifiers {
		if fc := fn(report); fc != nil {
			return *fc
		}
	}

	// Catch-all: check for any signs of cluster distress that didn't match
	// a specific pattern above (e.g., unrecognized waiting reason, unready
	// deployments, non-OOM terminated containers, high restart counts).
	if fc := classifyClusterDistress(report); fc != nil {
		return *fc
	}

	// The report is complete and no infrastructure issues were found.
	// The failure is likely in the test itself.
	if reportIsComplete(report) {
		return FailureClassification{
			Category:    CategoryTest,
			Subcategory: "test-failure",
			ErrorCode:   CodeTestFailure,
			Evidence:    []string{"cluster state appears healthy, failure is likely test-related"},
			Confidence:  ConfidenceMedium,
		}
	}

	return unknown()
}

// reportIsComplete returns true if the key sections were collected without errors.
// If any section errored, we can't be confident the cluster is actually healthy.
func reportIsComplete(report *clusterhealth.Report) bool {
	return report.Pods.Error == "" &&
		report.Nodes.Error == "" &&
		report.Events.Error == "" &&
		report.Quotas.Error == "" &&
		report.Deployments.Error == ""
}

// classifyFromPods checks container states and pod phases.
// Covers: image-pull, pod-startup, OOM subcategories.
func classifyFromPods(report *clusterhealth.Report) *FailureClassification {
	for _, pods := range report.Pods.Data.ByNamespace {
		for _, pod := range pods {
			for _, container := range pod.Containers {
				if match := matchesPattern(container.Waiting, waitingPatterns); match != nil {
					return &FailureClassification{
						Category:    CategoryInfrastructure,
						Subcategory: match.subcategory,
						ErrorCode:   match.errorCode,
						Evidence:    []string{fmt.Sprintf("container %s/%s waiting: %s", pod.Name, container.Name, container.Waiting)},
						Confidence:  ConfidenceMedium,
					}
				}
				if match := matchesPattern(container.Terminated, terminatedPatterns); match != nil {
					return &FailureClassification{
						Category:    CategoryInfrastructure,
						Subcategory: match.subcategory,
						ErrorCode:   match.errorCode,
						Evidence:    []string{fmt.Sprintf("container %s/%s terminated: %s", pod.Name, container.Name, container.Terminated)},
						Confidence:  ConfidenceMedium,
					}
				}
			}
			if pod.Phase == "Pending" {
				return &FailureClassification{
					Category:    CategoryInfrastructure,
					Subcategory: "pod-startup",
					ErrorCode:   CodePodStartup,
					Evidence:    []string{fmt.Sprintf("pod %s stuck in Pending", pod.Name)},
					Confidence:  ConfidenceHigh,
				}
			}
		}
	}
	return nil
}

// classifyFromEvents checks event reasons/messages for network and storage patterns.
// Covers: network, storage subcategories.
func classifyFromEvents(report *clusterhealth.Report) *FailureClassification {
	for _, event := range report.Events.Data.Events {
		if networkEventReasons[event.Reason] || containsNetworkPattern(event.Message) {
			return &FailureClassification{
				Category:    CategoryInfrastructure,
				Subcategory: "network",
				ErrorCode:   CodeNetwork,
				Evidence:    []string{fmt.Sprintf("event %s/%s: %s - %s", event.Kind, event.Name, event.Reason, event.Message)},
				Confidence:  ConfidenceMedium,
			}
		}
		if storageEventReasons[event.Reason] || containsStoragePattern(event.Message) {
			return &FailureClassification{
				Category:    CategoryInfrastructure,
				Subcategory: "storage",
				ErrorCode:   CodeStorage,
				Evidence:    []string{fmt.Sprintf("event %s/%s: %s - %s", event.Kind, event.Name, event.Reason, event.Message)},
				Confidence:  ConfidenceMedium,
			}
		}
	}
	return nil
}

// classifyFromQuotas checks resource quota violations.
// Covers: quota-oom subcategory.
func classifyFromQuotas(report *clusterhealth.Report) *FailureClassification {
	for _, quotas := range report.Quotas.Data.ByNamespace {
		for _, q := range quotas {
			if len(q.Exceeded) > 0 {
				return &FailureClassification{
					Category:    CategoryInfrastructure,
					Subcategory: "quota-oom",
					ErrorCode:   CodeQuotaOOM,
					Evidence:    []string{fmt.Sprintf("quota %s/%s exceeded: %v", q.Namespace, q.Name, q.Exceeded)},
					Confidence:  ConfidenceHigh,
				}
			}
		}
	}
	return nil
}

// classifyFromNodes checks node conditions.
// Covers: node-pressure subcategory.
func classifyFromNodes(report *clusterhealth.Report) *FailureClassification {
	for _, node := range report.Nodes.Data.Nodes {
		if node.UnhealthyReason != "" {
			return &FailureClassification{
				Category:    CategoryInfrastructure,
				Subcategory: "node-pressure",
				ErrorCode:   CodeNodePressure,
				Evidence:    []string{fmt.Sprintf("node %s: %s", node.Name, node.UnhealthyReason)},
				Confidence:  ConfidenceHigh,
			}
		}
	}
	return nil
}

// classifyClusterDistress checks for any signs of cluster problems that didn't
// match a specific pattern. This catches unrecognized container errors and unready
// deployments.
func classifyClusterDistress(report *clusterhealth.Report) *FailureClassification {
	for _, pods := range report.Pods.Data.ByNamespace {
		for _, pod := range pods {
			for _, c := range pod.Containers {
				if c.Waiting != "" {
					return &FailureClassification{
						Category:    CategoryInfrastructure,
						Subcategory: "cluster-distress",
						ErrorCode:   CodeInfraUnknown,
						Evidence:    []string{fmt.Sprintf("container %s/%s in unrecognized waiting state: %s", pod.Name, c.Name, c.Waiting)},
						Confidence:  ConfidenceLow,
					}
				}
				if c.Terminated != "" {
					return &FailureClassification{
						Category:    CategoryInfrastructure,
						Subcategory: "cluster-distress",
						ErrorCode:   CodeInfraUnknown,
						Evidence:    []string{fmt.Sprintf("container %s/%s terminated: %s", pod.Name, c.Name, c.Terminated)},
						Confidence:  ConfidenceLow,
					}
				}
			}
		}
	}

	// Check for unready deployments.
	for _, deploys := range report.Deployments.Data.ByNamespace {
		for _, d := range deploys {
			if d.Ready < d.Replicas {
				return &FailureClassification{
					Category:    CategoryInfrastructure,
					Subcategory: "cluster-distress",
					ErrorCode:   CodeInfraUnknown,
					Evidence:    []string{fmt.Sprintf("deployment %s/%s not ready: %d/%d replicas", d.Namespace, d.Name, d.Ready, d.Replicas)},
					Confidence:  ConfidenceLow,
				}
			}
		}
	}

	return nil
}

// unknown returns the default unclassifiable result.
func unknown() FailureClassification {
	return FailureClassification{
		Category:    CategoryUnknown,
		Subcategory: "unclassifiable",
		ErrorCode:   CodeUnclassifiable,
		Evidence:    []string{"no matching classification rule"},
		Confidence:  ConfidenceLow,
	}
}
