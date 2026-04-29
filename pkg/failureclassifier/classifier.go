package failureclassifier

import (
	"fmt"
	"strings"
	"time"

	"github.com/opendatahub-io/opendatahub-operator/pkg/clusterhealth"
)

// PendingThreshold is the minimum duration a pod must be in Pending phase
// before it is classified as "stuck pending". Pods pending for less than this
// are assumed to be starting up normally.
const PendingThreshold = 60 * time.Second

// Classify inspects a clusterhealth Report and categorizes the failure.
// If report is nil, returns unknown/unclassifiable/3000/low.
func Classify(report *clusterhealth.Report) FailureClassification {
	if report == nil {
		return unknown()
	}

	// classifyFromPods returns both a specific match (known pattern) and a
	// deferred distress signal (unrecognized issue). We use the specific
	// match immediately if found, and defer the distress signal until after
	// all other classifiers have had a chance to find something more specific.
	specific, podDistress := classifyFromPods(report)
	if specific != nil {
		return *specific
	}

	// Remaining section classifiers, ordered by priority (first match wins).
	sectionClassifiers := []func(*clusterhealth.Report) *FailureClassification{
		classifyFromEvents,
		classifyFromQuotas,
		classifyFromNodes,
		classifyFromOperator,
		classifyFromDSCI,
		classifyFromDSC,
	}

	for _, fn := range sectionClassifiers {
		if fc := fn(report); fc != nil {
			return *fc
		}
	}

	// Catch-all: use the deferred pod distress signal if we found one during
	// the pod scan, or check for unready deployments.
	if fc := classifyClusterDistress(report, podDistress); fc != nil {
		return *fc
	}

	// The report is complete and no infrastructure issues were found.
	// The failure is likely in the test itself.
	if report.Healthy() {
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

// classifyFromPods checks container states and pod phases in a single pass.
// Returns two values:
//   - specific: a classification from a known pattern (image-pull, OOM, etc.) — returned immediately by the caller.
//   - distress: the first unrecognized container issue found — deferred until after all other classifiers run.
func classifyFromPods(report *clusterhealth.Report) (*FailureClassification, *FailureClassification) {
	var distress *FailureClassification

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
					}, nil
				}
				if match := matchesPattern(container.Terminated, terminatedPatterns); match != nil {
					return &FailureClassification{
						Category:    CategoryInfrastructure,
						Subcategory: match.subcategory,
						ErrorCode:   match.errorCode,
						Evidence:    []string{fmt.Sprintf("container %s/%s terminated: %s", pod.Name, container.Name, container.Terminated)},
						Confidence:  ConfidenceMedium,
					}, nil
				}
				// Stash first unrecognized distress signal for deferred use.
				if distress == nil {
					if container.Waiting != "" {
						distress = &FailureClassification{
							Category:    CategoryInfrastructure,
							Subcategory: "cluster-distress",
							ErrorCode:   CodeInfraUnknown,
							Evidence:    []string{fmt.Sprintf("container %s/%s in unrecognized waiting state: %s", pod.Name, container.Name, container.Waiting)},
							Confidence:  ConfidenceLow,
						}
					} else if container.Terminated != "" && !isSuccessfulTermination(container.Terminated) {
						distress = &FailureClassification{
							Category:    CategoryInfrastructure,
							Subcategory: "cluster-distress",
							ErrorCode:   CodeInfraUnknown,
							Evidence:    []string{fmt.Sprintf("container %s/%s terminated: %s", pod.Name, container.Name, container.Terminated)},
							Confidence:  ConfidenceLow,
						}
					}
				}
			}
			if pod.Phase == "Pending" && !pod.CreatedAt.IsZero() && time.Since(pod.CreatedAt) > PendingThreshold {
				return &FailureClassification{
					Category:    CategoryInfrastructure,
					Subcategory: "pod-startup",
					ErrorCode:   CodePodStartup,
					Evidence:    []string{fmt.Sprintf("pod %s stuck in Pending for %s", pod.Name, time.Since(pod.CreatedAt).Truncate(time.Second))},
					Confidence:  ConfidenceHigh,
				}, nil
			}
		}
	}
	return nil, distress
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

// classifyClusterDistress uses the pre-computed pod distress signal (if any)
// and checks for unready deployments. This avoids re-iterating over pods.
func classifyClusterDistress(report *clusterhealth.Report, podDistress *FailureClassification) *FailureClassification {
	if podDistress != nil {
		return podDistress
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

// classifyFromOperator checks the operator deployment and pod status.
func classifyFromOperator(report *clusterhealth.Report) *FailureClassification {
	if d := report.Operator.Data.Deployment; d != nil {
		if d.Replicas == 0 {
			return &FailureClassification{
				Category:    CategoryInfrastructure,
				Subcategory: "operator",
				ErrorCode:   CodeOperator,
				Evidence:    []string{fmt.Sprintf("operator deployment %s scaled to 0 replicas", d.Name)},
				Confidence:  ConfidenceHigh,
			}
		}
		if d.Ready < d.Replicas {
			return &FailureClassification{
				Category:    CategoryInfrastructure,
				Subcategory: "operator",
				ErrorCode:   CodeOperator,
				Evidence:    []string{fmt.Sprintf("operator deployment %s not ready: %d/%d replicas", d.Name, d.Ready, d.Replicas)},
				Confidence:  ConfidenceHigh,
			}
		}
	}
	for _, pod := range report.Operator.Data.Pods {
		if pod.Phase != "Running" && pod.Phase != "Succeeded" {
			return &FailureClassification{
				Category:    CategoryInfrastructure,
				Subcategory: "operator",
				ErrorCode:   CodeOperator,
				Evidence:    []string{fmt.Sprintf("operator pod %s in phase %s", pod.Name, pod.Phase)},
				Confidence:  ConfidenceHigh,
			}
		}
	}
	return nil
}

// classifyFromDSCI checks the DSCInitialization CR conditions.
func classifyFromDSCI(report *clusterhealth.Report) *FailureClassification {
	return classifyFromCRConditions("DSCI", report.DSCI, CodeDSCI)
}

// classifyFromDSC checks the DataScienceCluster CR conditions.
func classifyFromDSC(report *clusterhealth.Report) *FailureClassification {
	return classifyFromCRConditions("DSC", report.DSC, CodeDSC)
}

// classifyFromCRConditions checks a CR conditions section for unhealthy conditions.
// The Error field in clusterhealth sections can indicate either a collection failure
// or an unhealthy CR state, so we require both a non-empty Error (something is wrong)
// and a non-empty Name (the CR was actually found) before classifying.
func classifyFromCRConditions(name string, section clusterhealth.SectionResult[clusterhealth.CRConditionsSection], errorCode int) *FailureClassification {
	if section.Error == "" || section.Data.Name == "" {
		return nil
	}
	return &FailureClassification{
		Category:    CategoryInfrastructure,
		Subcategory: strings.ToLower(name) + "-unhealthy",
		ErrorCode:   errorCode,
		Evidence:    []string{fmt.Sprintf("%s %s unhealthy: %s", name, section.Data.Name, section.Error)},
		Confidence:  ConfidenceMedium,
	}
}

// isSuccessfulTermination returns true if the terminated string indicates
// a container that completed successfully (exit code 0). The terminated
// string format from clusterhealth is "{Reason} (exit {Code})[: {Message}]".
func isSuccessfulTermination(terminated string) bool {
	return strings.Contains(terminated, "(exit 0)")
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
