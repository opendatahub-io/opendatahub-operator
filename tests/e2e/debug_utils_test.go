package e2e_test

import (
	"context"
	"log"
	"sort"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

var globalDebugClient client.Client

// SetGlobalDebugClient sets the Kubernetes client for global debugging.
// This should be called during test setup to enable panic debugging.
func SetGlobalDebugClient(c client.Client) {
	globalDebugClient = c
}

// HandleGlobalPanic is a panic recovery handler that runs comprehensive
// cluster diagnostics when tests panic. It should be called with defer
// in TestMain or BeforeSuite.
func HandleGlobalPanic() {
	if r := recover(); r != nil {
		log.Printf("PANIC DETECTED: %v", r)
		log.Printf("Running comprehensive cluster diagnostics...")

		if globalDebugClient != nil {
			runAllDiagnostics()
		} else {
			log.Printf("No client available for debugging")
		}

		log.Printf("Diagnostics complete, re-panicking...")
		panic(r)
	}
}

// HandleTestFailure runs comprehensive cluster diagnostics when tests fail
// or timeout. This should be called when a test fails to help diagnose
// cluster-related issues.
func HandleTestFailure(testName string) {
	log.Printf("TEST FAILURE DETECTED: %s", testName)
	log.Printf("Running comprehensive cluster diagnostics...")

	if globalDebugClient != nil {
		runAllDiagnostics()
	} else {
		log.Printf("No client available for debugging")
	}

	log.Printf("Diagnostics complete for failed test: %s", testName)
}

// runAllDiagnostics executes all diagnostic functions in order.
func runAllDiagnostics() {
	debugClusterState()
	debugNamespaceResources()
	debugOperatorStatus()
	debugRecentEvents()
	debugResourceQuotas()
}

// debugClusterState checks overall cluster health including node status and resources.
func debugClusterState() {
	log.Printf("=== CLUSTER STATE ===")

	nodes := &corev1.NodeList{}
	if err := globalDebugClient.List(context.TODO(), nodes); err != nil {
		log.Printf("Failed to list nodes: %v", err)
		return
	}

	for _, node := range nodes.Items {
		log.Printf("Node: %s", node.Name)

		// Check node conditions
		for _, condition := range node.Status.Conditions {
			if condition.Status != corev1.ConditionTrue {
				log.Printf("  Condition %s: %s - %s", condition.Type, condition.Status, condition.Message)
			}

			// Check resource pressure
			if (condition.Type == corev1.NodeMemoryPressure ||
				condition.Type == corev1.NodeDiskPressure ||
				condition.Type == corev1.NodePIDPressure) && condition.Status == corev1.ConditionTrue {
				log.Printf("  RESOURCE PRESSURE %s: %s", condition.Type, condition.Message)
			}
		}

		logResourceAllocation(node.Status.Allocatable, node.Status.Capacity)
	}
}

// logResourceAllocation logs CPU and memory allocation percentages.
func logResourceAllocation(allocatable, capacity corev1.ResourceList) {
	// CPU allocation
	if cpuCap := capacity[corev1.ResourceCPU]; !cpuCap.IsZero() {
		cpuAlloc := allocatable[corev1.ResourceCPU]
		cpuPct := float64(cpuAlloc.MilliValue()) / float64(cpuCap.MilliValue()) * 100
		log.Printf("  CPU: %s/%s (%.1f%% available)", cpuAlloc.String(), cpuCap.String(), cpuPct)
	}

	// Memory allocation
	if memCap := capacity[corev1.ResourceMemory]; !memCap.IsZero() {
		memAlloc := allocatable[corev1.ResourceMemory]
		memPct := float64(memAlloc.Value()) / float64(memCap.Value()) * 100
		log.Printf("  Memory: %s/%s (%.1f%% available)", memAlloc.String(), memCap.String(), memPct)
	}
}

// debugNamespaceResources checks deployments and pods in key namespaces.
func debugNamespaceResources() {
	log.Printf("=== NAMESPACE RESOURCES ===")

	namespaces := []string{"opendatahub", "openshift-operators", "kube-system"}
	for _, ns := range namespaces {
		log.Printf("Namespace: %s", ns)

		// Check deployments
		deployments := &appsv1.DeploymentList{}
		if err := globalDebugClient.List(context.TODO(), deployments, client.InNamespace(ns)); err != nil {
			log.Printf("  Failed to list deployments: %v", err)
			continue
		}

		for _, deploy := range deployments.Items {
			if deploy.Status.ReadyReplicas != deploy.Status.Replicas {
				log.Printf("  Deployment %s: %d/%d (NOT READY)",
					deploy.Name, deploy.Status.ReadyReplicas, deploy.Status.Replicas)

				for _, condition := range deploy.Status.Conditions {
					if condition.Status != corev1.ConditionTrue {
						log.Printf("    %s: %s - %s", condition.Type, condition.Status, condition.Message)
					}
				}
			}
		}

		// Check failed pods
		pods := &corev1.PodList{}
		if err := globalDebugClient.List(context.TODO(), pods, client.InNamespace(ns)); err != nil {
			log.Printf("  Failed to list pods: %v", err)
			continue
		}

		for _, pod := range pods.Items {
			if pod.Status.Phase != corev1.PodRunning &&
				pod.Status.Phase != corev1.PodSucceeded &&
				pod.Status.Phase != corev1.PodPending {
				log.Printf("  Pod %s: %s", pod.Name, pod.Status.Phase)

				// Check pod conditions
				for _, condition := range pod.Status.Conditions {
					if condition.Status != corev1.ConditionTrue {
						log.Printf("    %s: %s - %s", condition.Type, condition.Status, condition.Message)
					}
				}

				logContainerStates(pod.Status.ContainerStatuses)
			}
		}
	}
}

// logContainerStates logs waiting or terminated container states.
func logContainerStates(containerStatuses []corev1.ContainerStatus) {
	for _, containerStatus := range containerStatuses {
		if containerStatus.State.Waiting != nil {
			log.Printf("    Container %s waiting: %s - %s",
				containerStatus.Name,
				containerStatus.State.Waiting.Reason,
				containerStatus.State.Waiting.Message)
		}
		if containerStatus.State.Terminated != nil {
			log.Printf("    Container %s terminated: %s - %s (exit: %d)",
				containerStatus.Name,
				containerStatus.State.Terminated.Reason,
				containerStatus.State.Terminated.Message,
				containerStatus.State.Terminated.ExitCode)
		}
	}
}

// debugOperatorStatus checks the OpenDataHub operator deployment and pods.
func debugOperatorStatus() {
	log.Printf("=== OPERATOR STATUS ===")

	// Check main operator deployment
	operatorDeploy := &appsv1.Deployment{}
	err := globalDebugClient.Get(context.TODO(),
		types.NamespacedName{Name: "opendatahub-operator-controller-manager", Namespace: "opendatahub"},
		operatorDeploy)

	if err != nil {
		log.Printf("Cannot find operator deployment: %v", err)
	} else {
		log.Printf("Operator deployment: %d/%d ready",
			operatorDeploy.Status.ReadyReplicas, operatorDeploy.Status.Replicas)

		for _, condition := range operatorDeploy.Status.Conditions {
			if condition.Status != corev1.ConditionTrue {
				log.Printf("  %s: %s - %s", condition.Type, condition.Status, condition.Message)
			}
		}
	}

	// Check operator pods
	pods := &corev1.PodList{}
	err = globalDebugClient.List(context.TODO(), pods,
		client.InNamespace("opendatahub"),
		client.MatchingLabels{"app.kubernetes.io/name": "opendatahub-operator"})

	if err != nil {
		log.Printf("Failed to list operator pods: %v", err)
	} else {
		for _, pod := range pods.Items {
			if pod.Status.Phase != corev1.PodRunning {
				log.Printf("Operator pod %s: %s", pod.Name, pod.Status.Phase)

				for _, containerStatus := range pod.Status.ContainerStatuses {
					if containerStatus.RestartCount > 0 {
						log.Printf("  Container %s restarted %d times",
							containerStatus.Name, containerStatus.RestartCount)
					}
				}
			}
		}
	}

	debugDSCIStatus()
}

// debugDSCIStatus checks DataScienceClusterInitialization and DataScienceCluster status.
func debugDSCIStatus() {
	log.Printf("=== DSCI/DSC STATUS ===")

	// Check DSCI
	dscis := &unstructured.UnstructuredList{}
	dscis.SetGroupVersionKind(gvk.DSCInitialization)
	if err := globalDebugClient.List(context.TODO(), dscis); err != nil {
		log.Printf("Failed to list DSCI: %v", err)
	} else {
		for _, dsci := range dscis.Items {
			log.Printf("DSCI %s:", dsci.GetName())
			logUnhealthyConditions(dsci.Object)
		}
	}

	// Check DSC
	dscs := &unstructured.UnstructuredList{}
	dscs.SetGroupVersionKind(gvk.DataScienceCluster)
	if err := globalDebugClient.List(context.TODO(), dscs); err != nil {
		log.Printf("Failed to list DSC: %v", err)
	} else {
		for _, dsc := range dscs.Items {
			log.Printf("DSC %s:", dsc.GetName())
			logUnhealthyConditions(dsc.Object)
		}
	}
}

// logUnhealthyConditions logs any conditions that are not "True".
func logUnhealthyConditions(obj map[string]interface{}) {
	conditions, found, _ := unstructured.NestedSlice(obj, "status", "conditions")
	if !found {
		return
	}

	for _, conditionRaw := range conditions {
		if condition, ok := conditionRaw.(map[string]interface{}); ok {
			condType, _ := condition["type"].(string)
			status, _ := condition["status"].(string)

			if status != "True" {
				message, _ := condition["message"].(string)
				log.Printf("  %s: %s - %s", condType, status, message)
			}
		}
	}
}

// debugRecentEvents shows recent events from key namespaces.
func debugRecentEvents() {
	log.Printf("=== RECENT EVENTS (last 5 minutes) ===")

	namespaces := []string{"opendatahub", "openshift-operators", "kube-system"}
	cutoff := time.Now().Add(-5 * time.Minute)

	for _, ns := range namespaces {
		events := &corev1.EventList{}
		if err := globalDebugClient.List(context.TODO(), events, client.InNamespace(ns)); err != nil {
			continue
		}

		// Filter recent events
		var recentEvents []corev1.Event
		for _, event := range events.Items {
			if event.LastTimestamp.After(cutoff) {
				recentEvents = append(recentEvents, event)
			}
		}

		if len(recentEvents) == 0 {
			continue
		}

		// Sort by timestamp (newest first)
		sort.Slice(recentEvents, func(i, j int) bool {
			return recentEvents[i].LastTimestamp.After(recentEvents[j].LastTimestamp.Time)
		})

		log.Printf("Namespace %s:", ns)
		maxEvents := 10
		if len(recentEvents) < maxEvents {
			maxEvents = len(recentEvents)
		}

		for _, event := range recentEvents[:maxEvents] {
			eventType := "INFO"
			if event.Type == "Warning" {
				eventType = "WARN"
			}
			log.Printf("  %s %s %s/%s: %s - %s",
				event.LastTimestamp.Format("15:04:05"),
				eventType,
				event.InvolvedObject.Kind, event.InvolvedObject.Name,
				event.Reason, event.Message)
		}
	}
}

// debugResourceQuotas checks for resource quota violations.
func debugResourceQuotas() {
	log.Printf("=== RESOURCE QUOTAS ===")

	namespaces := []string{"opendatahub", "openshift-operators"}
	for _, ns := range namespaces {
		quotas := &corev1.ResourceQuotaList{}
		if err := globalDebugClient.List(context.TODO(), quotas, client.InNamespace(ns)); err != nil {
			continue
		}

		for _, quota := range quotas.Items {
			log.Printf("Namespace %s, ResourceQuota %s:", ns, quota.Name)
			for resource, used := range quota.Status.Used {
				if hard, exists := quota.Status.Hard[resource]; exists {
					if used.Value() >= hard.Value() {
						log.Printf("  QUOTA EXCEEDED %s: %s/%s", resource, used.String(), hard.String())
					}
				}
			}
		}
	}
}
