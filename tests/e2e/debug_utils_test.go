package e2e_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlcfg "sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

const (
	// Container waiting reason constants (commonly used values in Kubernetes).
	CrashLoopBackOff           = "CrashLoopBackOff"
	ImagePullBackOff           = "ImagePullBackOff"
	ErrImagePull               = "ErrImagePull"
	InvalidImageName           = "InvalidImageName"
	CreateContainerConfigError = "CreateContainerConfigError"
	CreateContainerError       = "CreateContainerError"

	// Condition status constants for unstructured objects.
	conditionStatusTrue = "True"
)

var (
	// Regex patterns for common secrets in logs.
	authBearerPattern = regexp.MustCompile(`(?i)(Authorization:\s*Bearer\s+)[^\s]+`)
	passwordPattern   = regexp.MustCompile(`(?i)(password[=:\s]+)[^\s&"']+`)
	tokenPattern      = regexp.MustCompile(`(?i)(token[=:\s]+)[^\s&"']+`)
	secretPattern     = regexp.MustCompile(`(?i)(secret[=:\s]+)[^\s&"']+`)
	apiKeyPattern     = regexp.MustCompile(`(?i)(api[_-]?key[=:\s]+)[^\s&"']+`)
	accessKeyPattern  = regexp.MustCompile(`(?i)(access[_-]?key[=:\s]+)[^\s&"']+`)
)

var (
	globalDebugClient client.Client
	debugClientOnce   sync.Once
	debugMutex        sync.Mutex
	// dedupe keys: "panic" or "test:<name>".
	diagnosticKeys sync.Map
	// lastPanicDiagTS helps suppress duplicate failure diagnostics immediately after a panic.
	lastPanicDiagTS atomic.Int64
)

// SetGlobalDebugClient sets the Kubernetes client for global debugging.
// This should be called during test setup to enable panic debugging.
// Uses sync.Once to ensure the client is set exactly once, preventing data races.
func SetGlobalDebugClient(c client.Client) {
	debugClientOnce.Do(func() {
		globalDebugClient = c
	})
}

// redactSensitiveInfo removes common secrets and tokens from log content.
func redactSensitiveInfo(logContent string) string {
	// Apply all redaction patterns
	result := authBearerPattern.ReplaceAllString(logContent, "${1}[REDACTED]")
	result = passwordPattern.ReplaceAllString(result, "${1}[REDACTED]")
	result = tokenPattern.ReplaceAllString(result, "${1}[REDACTED]")
	result = secretPattern.ReplaceAllString(result, "${1}[REDACTED]")
	result = apiKeyPattern.ReplaceAllString(result, "${1}[REDACTED]")
	result = accessKeyPattern.ReplaceAllString(result, "${1}[REDACTED]")

	return result
}

// HandleGlobalPanic is a panic recovery handler that runs comprehensive
// cluster diagnostics when tests panic. It should be called with defer
// in TestMain or BeforeSuite.
func HandleGlobalPanic() {
	if r := recover(); r != nil {
		log.Printf("=== PANIC DETECTED: %v ===", r)
		runDiagnosticsOnce("panic") // key = "panic"
		lastPanicDiagTS.Store(time.Now().UnixNano())
		log.Printf("Diagnostics complete, re-panicking...")
		panic(r)
	}
}

// HandleTestFailure runs comprehensive cluster diagnostics when tests fail
// or timeout. This should be called when a test fails to help diagnose
// cluster-related issues.
func HandleTestFailure(testName string) {
	log.Printf("TEST FAILURE DETECTED: %s", testName)
	runDiagnosticsOnce("test:" + testName) // key = "test:<name>"
	log.Printf("Diagnostics complete for failed test: %s", testName)
}

// runDiagnosticsOnce ensures diagnostics run once per provided key (e.g., "panic", "test:<name>").
func runDiagnosticsOnce(key string) {
	if _, loaded := diagnosticKeys.LoadOrStore(key, struct{}{}); loaded {
		log.Printf("Diagnostics already ran for key %q", key)
		return
	}

	log.Printf("=== RUNNING CLUSTER DIAGNOSTICS (triggered by %s) ===", key)

	debugMutex.Lock()
	defer debugMutex.Unlock()

	if globalDebugClient == nil {
		log.Printf("ERROR: No Kubernetes client available for debugging!")
		return
	}

	runAllDiagnostics()
}

// runAllDiagnostics executes all diagnostic functions in order.
func runAllDiagnostics() {
	debugClusterState()
	debugNamespaceResources()
	debugOperatorStatus()
	debugDSCIStatus()
	debugRecentEvents()
	debugResourceQuotas()
}

// getOperatorPods returns operator pods by trying both OpenDataHub and RHODS label selectors.
// This platform-agnostic approach avoids complex platform detection in debug context.
func getOperatorPods() (*corev1.PodList, error) {
	// Try OpenDataHub selector first
	odhPods := &corev1.PodList{}
	err := globalDebugClient.List(context.TODO(), odhPods,
		client.InNamespace(testOpts.operatorNamespace),
		client.MatchingLabels{"control-plane": "controller-manager"})

	if err != nil {
		return nil, err
	}

	// Try RHODS selector
	rhoadsPods := &corev1.PodList{}
	err = globalDebugClient.List(context.TODO(), rhoadsPods,
		client.InNamespace(testOpts.operatorNamespace),
		client.MatchingLabels{"name": "rhods-operator"})

	if err != nil {
		return nil, err
	}

	// Combine results (usually only one will have pods)
	allPods := &corev1.PodList{}
	allPods.Items = append(allPods.Items, odhPods.Items...)
	allPods.Items = append(allPods.Items, rhoadsPods.Items...)

	return allPods, nil
}

// debugClusterState checks overall cluster health including node status and resources.
func debugClusterState() {
	log.Printf("=== CLUSTER STATE ===")

	nodes := &corev1.NodeList{}
	if err := globalDebugClient.List(context.TODO(), nodes); err != nil {
		log.Printf("Failed to list nodes: %v", err)
		return
	}

	if len(nodes.Items) == 0 {
		log.Printf("No nodes found in cluster")
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

// logResourceAllocation logs node resource capacity and allocatable amounts.
func logResourceAllocation(allocatable, capacity corev1.ResourceList) {
	// CPU resources (show both in millicores for consistency)
	if cpuCap := capacity[corev1.ResourceCPU]; !cpuCap.IsZero() {
		cpuAlloc := allocatable[corev1.ResourceCPU]
		log.Printf("  CPU: %dm allocatable (of %dm total)", cpuAlloc.MilliValue(), cpuCap.MilliValue())
	}

	// Memory resources
	if memCap := capacity[corev1.ResourceMemory]; !memCap.IsZero() {
		memAlloc := allocatable[corev1.ResourceMemory]
		log.Printf("  Memory: %s allocatable (of %s total)", memAlloc.String(), memCap.String())
	}
}

// debugNamespaceResources checks deployments and pods in key namespaces.
func debugNamespaceResources() {
	log.Printf("=== NAMESPACE RESOURCES ===")

	namespaces := []string{testOpts.appsNamespace, testOpts.operatorNamespace, "kube-system"}
	for _, ns := range namespaces {
		log.Printf("Namespace: %s", ns)
		problemsFound := false

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
				problemsFound = true

				for _, condition := range deploy.Status.Conditions {
					if condition.Status != corev1.ConditionTrue {
						log.Printf("    %s: %s - %s", condition.Type, condition.Status, condition.Message)
					}
				}

				// Show related pods for this deployment
				debugDeploymentPods(deploy.Name, ns)
			}
		}

		// Check failed pods
		pods := &corev1.PodList{}
		if err := globalDebugClient.List(context.TODO(), pods, client.InNamespace(ns)); err != nil {
			log.Printf("  Failed to list pods: %v", err)
			continue
		}

		// Optional: Show summary of pods found (can be commented out for cleaner output)
		// log.Printf("  Found %d pods in namespace %s", len(pods.Items), ns)
		for _, pod := range pods.Items {
			podHasIssues := false

			// Check for non-healthy pod phases
			if pod.Status.Phase != corev1.PodRunning &&
				pod.Status.Phase != corev1.PodSucceeded &&
				pod.Status.Phase != corev1.PodPending {
				podHasIssues = true
			}

			// Also check for pods with restarting or failing containers (even if pod is "Running")
			if pod.Status.Phase == corev1.PodRunning {
				for _, containerStatus := range pod.Status.ContainerStatuses {
					if containerStatus.RestartCount > 0 ||
						!containerStatus.Ready ||
						containerStatus.State.Waiting != nil ||
						containerStatus.State.Terminated != nil {
						podHasIssues = true
						break
					}
				}
			}

			if podHasIssues {
				log.Printf("  Pod %s: %s", pod.Name, pod.Status.Phase)
				problemsFound = true

				// Run comprehensive pod diagnostics for problematic pods
				capturePodDiagnostics(context.TODO(), ns, pod.Name)
			}
		}

		if !problemsFound {
			log.Printf("  No problematic deployments or pods found")
		}
	}
}

// debugDeploymentPods shows detailed status of pods belonging to a specific deployment.
func debugDeploymentPods(deploymentName, namespace string) {
	pods := &corev1.PodList{}

	// Try common label patterns for deployment pods
	selectors := []client.MatchingLabels{
		{"app": deploymentName},
		{"app.kubernetes.io/name": deploymentName},
		{"deployment": deploymentName},
	}

	var allPods []corev1.Pod
	podNames := make(map[string]bool)

	for _, sel := range selectors {
		if err := globalDebugClient.List(context.TODO(), pods, client.InNamespace(namespace), sel); err == nil {
			for _, pod := range pods.Items {
				if !podNames[pod.Name] {
					allPods = append(allPods, pod)
					podNames[pod.Name] = true
				}
			}
			// If we found pods with labels, no need to do owner reference lookup
			if len(allPods) > 0 {
				break
			}
		}
	}

	// If no pods found with labels, try owner reference lookup
	if len(allPods) == 0 {
		if err := globalDebugClient.List(context.TODO(), pods, client.InNamespace(namespace)); err == nil {
			for _, pod := range pods.Items {
				if !podNames[pod.Name] { // Check duplicates here too
					for _, owner := range pod.OwnerReferences {
						if owner.Kind == "ReplicaSet" {
							// Get the ReplicaSet to check if it belongs to our deployment
							rs := &appsv1.ReplicaSet{}
							if err := globalDebugClient.Get(context.TODO(), client.ObjectKey{
								Name:      owner.Name,
								Namespace: namespace,
							}, rs); err == nil {
								for _, rsOwner := range rs.OwnerReferences {
									if rsOwner.Kind == "Deployment" && rsOwner.Name == deploymentName {
										allPods = append(allPods, pod)
										podNames[pod.Name] = true
										break
									}
								}
							}
						}
					}
				}
			}
		}
	}

	if len(allPods) == 0 {
		log.Printf("    No pods found for deployment %s", deploymentName)
		return
	}

	for _, pod := range allPods {
		log.Printf("    Pod %s: %s", pod.Name, pod.Status.Phase)

		// Run comprehensive pod diagnostics
		capturePodDiagnostics(context.TODO(), namespace, pod.Name)
	}
}

// debugOperatorStatus checks the OpenDataHub operator deployment and pods.
func debugOperatorStatus() {
	log.Printf("=== OPERATOR STATUS ===")

	// Check main operator deployment
	operatorDeploy := &appsv1.Deployment{}
	err := globalDebugClient.Get(context.TODO(),
		types.NamespacedName{Name: getControllerDeploymentName(), Namespace: testOpts.operatorNamespace},
		operatorDeploy)

	if err != nil {
		log.Printf("Cannot find operator deployment: %v", err)
		return
	}

	log.Printf("Operator deployment: %d/%d ready", operatorDeploy.Status.ReadyReplicas, operatorDeploy.Status.Replicas)
	for _, condition := range operatorDeploy.Status.Conditions {
		if condition.Status != corev1.ConditionTrue {
			log.Printf("  %s: %s - %s", condition.Type, condition.Status, condition.Message)
		}
	}

	// Check operator pods
	pods, err := getOperatorPods()
	if err != nil {
		log.Printf("Failed to retrieve logs: %v", err)
		return
	}

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

func getDSCI() *unstructured.Unstructured {
	dsci := &unstructured.Unstructured{}
	dsci.SetGroupVersionKind(gvk.DSCInitialization)
	err := globalDebugClient.Get(context.TODO(), types.NamespacedName{Name: dsciInstanceName}, dsci)
	if err != nil && !errors.IsNotFound(err) {
		log.Printf("Failed to get DSCI: %v", err)
		return nil
	}

	if errors.IsNotFound(err) {
		log.Printf("No DSCI instance found")
		return nil
	}
	return dsci
}

// debugDSCIStatus checks DataScienceClusterInitialization and DataScienceCluster status.
func debugDSCIStatus() {
	log.Printf("=== DSCI/DSC STATUS ===")

	// Check DSCI (prerequisite for DSC)
	dsci := getDSCI()
	if dsci != nil {
		log.Printf("DSCI %s:", dsci.GetName())
		logConditions(dsci.Object)
	}

	// Check DSC (singleton resource - depends on DSCI)
	dsc := &unstructured.Unstructured{}
	dsc.SetGroupVersionKind(gvk.DataScienceCluster)
	err := globalDebugClient.Get(context.TODO(), types.NamespacedName{Name: dscInstanceName}, dsc)
	if err != nil && !errors.IsNotFound(err) {
		log.Printf("Failed to get DSC: %v", err)
		return
	}

	if errors.IsNotFound(err) {
		log.Printf("No DSC instance found")
		return
	}

	log.Printf("DSC %s:", dsc.GetName())
	logConditions(dsc.Object)
}

// logConditions logs any conditions that are not "True".
func logConditions(obj map[string]interface{}) {
	conditions, found, _ := unstructured.NestedSlice(obj, "status", "conditions")
	if !found {
		return
	}

	for _, conditionRaw := range conditions {
		if condition, ok := conditionRaw.(map[string]interface{}); ok {
			condType, _ := condition["type"].(string)
			status, _ := condition["status"].(string)

			if status != conditionStatusTrue {
				message, _ := condition["message"].(string)
				log.Printf("  %s: %s - %s", condType, status, message)
			}
		}
	}
}

// debugRecentEvents shows recent events from key namespaces.
func debugRecentEvents() {
	log.Printf("=== RECENT EVENTS (last 5 minutes) ===")

	namespaces := []string{testOpts.appsNamespace, testOpts.operatorNamespace, "kube-system"}
	cutoff := time.Now().Add(-5 * time.Minute)
	hasEvents := false

	for _, ns := range namespaces {
		events := &corev1.EventList{}
		if err := globalDebugClient.List(context.TODO(), events, client.InNamespace(ns)); err != nil {
			log.Printf("Failed to list events in namespace %s: %v", ns, err)
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

		hasEvents = true
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

	if !hasEvents {
		log.Printf("No recent events found in monitored namespaces")
	}
}

// debugResourceQuotas checks for resource quota violations.
func debugResourceQuotas() {
	log.Printf("=== RESOURCE QUOTAS ===")

	namespaces := []string{testOpts.appsNamespace, testOpts.operatorNamespace}
	hasQuotas := false
	hasViolations := false

	for _, ns := range namespaces {
		quotas := &corev1.ResourceQuotaList{}
		if err := globalDebugClient.List(context.TODO(), quotas, client.InNamespace(ns)); err != nil {
			log.Printf("Failed to list resource quotas in namespace %s: %v", ns, err)
			continue
		}

		if len(quotas.Items) == 0 {
			continue
		}

		hasQuotas = true
		for _, quota := range quotas.Items {
			log.Printf("Namespace %s, ResourceQuota %s:", ns, quota.Name)
			quotaHasViolations := false
			for resource, used := range quota.Status.Used {
				if hard, exists := quota.Status.Hard[resource]; exists {
					if used.Value() >= hard.Value() {
						log.Printf("  QUOTA EXCEEDED %s: %s/%s", resource, used.String(), hard.String())
						hasViolations = true
						quotaHasViolations = true
					}
				}
			}
			if !quotaHasViolations {
				log.Printf("  No quota violations detected")
			}
		}

		// Print pods in namespace with quotas
		pods := &corev1.PodList{}
		if err := globalDebugClient.List(context.TODO(), pods, client.InNamespace(ns)); err != nil {
			log.Printf("Failed to list pods in namespace %s: %v", ns, err)
		} else {
			log.Printf("Pods in namespace %s (%d total):", ns, len(pods.Items))
			for _, pod := range pods.Items {
				log.Printf("  Pod %s: Phase=%s", pod.Name, pod.Status.Phase)
			}
		}
	}

	if !hasQuotas {
		log.Printf("No resource quotas found in monitored namespaces")
		return
	}

	if !hasViolations {
		log.Printf("All resource quotas are within limits")
		return
	}
}

// capturePodDiagnostics provides comprehensive diagnostics for a specific pod,
// including container states, logs, events, and resource usage.
func capturePodDiagnostics(ctx context.Context, namespace, podName string) {
	log.Printf("=== POD DIAGNOSTICS: %s/%s ===", namespace, podName)

	// Get pod details
	pod := &corev1.Pod{}
	if err := globalDebugClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: podName}, pod); err != nil {
		log.Printf("Failed to get pod: %v", err)
		return
	}

	// Show pod phase and start time
	podAge := time.Since(pod.CreationTimestamp.Time).Round(time.Second)
	log.Printf("Pod Phase: %s (age: %s)", pod.Status.Phase, podAge)

	// Show pod conditions
	log.Printf("Pod Conditions:")
	for _, condition := range pod.Status.Conditions {
		statusSymbol := "✓"
		if condition.Status != corev1.ConditionTrue {
			statusSymbol = "✗"
		}
		log.Printf("  %s %s: %s", statusSymbol, condition.Type, condition.Status)
		if condition.Message != "" && condition.Status != corev1.ConditionTrue {
			log.Printf("    Message: %s", condition.Message)
		}
		if condition.Reason != "" && condition.Status != corev1.ConditionTrue {
			log.Printf("    Reason: %s", condition.Reason)
		}
	}

	// Show detailed container states
	log.Printf("Container Status:")
	for _, containerStatus := range pod.Status.ContainerStatuses {
		captureSingleContainerDiagnostics(ctx, namespace, podName, pod, containerStatus)
	}

	// Show init container states if any failed
	for _, initStatus := range pod.Status.InitContainerStatuses {
		if !initStatus.Ready || initStatus.RestartCount > 0 ||
			initStatus.State.Waiting != nil || initStatus.State.Terminated != nil {
			log.Printf("Init Container Status:")
			captureSingleContainerDiagnostics(ctx, namespace, podName, pod, initStatus)
		}
	}

	// Show pod events
	capturePodEvents(ctx, namespace, podName)

	// Show resource requests and limits
	captureResourceInfo(pod)

	log.Printf("=== END POD DIAGNOSTICS ===")
}

// captureSingleContainerDiagnostics captures detailed information about a single container.
func captureSingleContainerDiagnostics(ctx context.Context, namespace, podName string, pod *corev1.Pod, containerStatus corev1.ContainerStatus) {
	log.Printf("  Container: %s", containerStatus.Name)
	log.Printf("    Ready: %v", containerStatus.Ready)
	log.Printf("    Restart Count: %d", containerStatus.RestartCount)
	log.Printf("    Image: %s", containerStatus.Image)

	// Show container state with timing
	switch {
	case containerStatus.State.Running != nil:
		startTime := containerStatus.State.Running.StartedAt.Time
		runningFor := time.Since(startTime).Round(time.Second)
		log.Printf("    State: Running (started %s ago)", runningFor)
	case containerStatus.State.Waiting != nil:
		log.Printf("    State: Waiting")
		log.Printf("      Reason: %s", containerStatus.State.Waiting.Reason)
		if containerStatus.State.Waiting.Message != "" {
			log.Printf("      Message: %s", containerStatus.State.Waiting.Message)
		}
	case containerStatus.State.Terminated != nil:
		log.Printf("    State: Terminated")
		log.Printf("      Reason: %s", containerStatus.State.Terminated.Reason)
		log.Printf("      Exit Code: %d", containerStatus.State.Terminated.ExitCode)
		if containerStatus.State.Terminated.Message != "" {
			log.Printf("      Message: %s", containerStatus.State.Terminated.Message)
		}
	}

	// Show last termination state if container has restarted
	if containerStatus.LastTerminationState.Terminated != nil {
		term := containerStatus.LastTerminationState.Terminated
		log.Printf("    Last Termination:")
		log.Printf("      Reason: %s", term.Reason)
		log.Printf("      Exit Code: %d", term.ExitCode)
		if term.Message != "" {
			log.Printf("      Message: %s", term.Message)
		}
	}

	// Show readiness probe configuration from pod spec
	captureReadinessProbeInfo(pod, containerStatus.Name)

	// Get container logs if not ready or has restarted
	if !containerStatus.Ready || containerStatus.RestartCount > 0 ||
		containerStatus.State.Waiting != nil || containerStatus.State.Terminated != nil {
		// Get current logs
		log.Printf("    === Recent Logs (last 100 lines) ===")
		logs, err := retrievePodLogsWithTail(ctx, namespace, podName, containerStatus.Name, false, 100)
		switch {
		case err != nil:
			log.Printf("    Failed to retrieve logs: %v", err)
		case logs == "":
			log.Printf("    No logs available")
		default:
			redactedLogs := redactSensitiveInfo(logs)
			logLines := strings.Split(strings.TrimSpace(redactedLogs), "\n")
			for _, line := range logLines {
				if strings.TrimSpace(line) != "" {
					log.Printf("    %s", line)
				}
			}
		}
		log.Printf("    === End Logs ===")

		// Get previous logs if restarted
		if containerStatus.RestartCount > 0 {
			log.Printf("    === Previous Logs (before restart, last 50 lines) ===")
			prevLogs, err := retrievePodLogsWithTail(ctx, namespace, podName, containerStatus.Name, true, 50)
			switch {
			case err != nil:
				log.Printf("    Failed to retrieve previous logs: %v", err)
			case prevLogs == "":
				log.Printf("    No previous logs available")
			default:
				redactedLogs := redactSensitiveInfo(prevLogs)
				logLines := strings.Split(strings.TrimSpace(redactedLogs), "\n")
				for _, line := range logLines {
					if strings.TrimSpace(line) != "" {
						log.Printf("    %s", line)
					}
				}
			}
			log.Printf("    === End Previous Logs ===")
		}
	}
}

// captureReadinessProbeInfo extracts and displays readiness probe configuration from pod spec.
func captureReadinessProbeInfo(pod *corev1.Pod, containerName string) {
	for _, container := range pod.Spec.Containers {
		if container.Name == containerName && container.ReadinessProbe != nil {
			log.Printf("    Readiness Probe:")
			probe := container.ReadinessProbe

			// Show probe type and details
			switch {
			case probe.HTTPGet != nil:
				log.Printf("      Type: HTTP GET")
				log.Printf("      Path: %s", probe.HTTPGet.Path)
				log.Printf("      Port: %v", probe.HTTPGet.Port)
				if probe.HTTPGet.Scheme != "" {
					log.Printf("      Scheme: %s", probe.HTTPGet.Scheme)
				}
			case probe.TCPSocket != nil:
				log.Printf("      Type: TCP Socket")
				log.Printf("      Port: %v", probe.TCPSocket.Port)
			case probe.Exec != nil:
				log.Printf("      Type: Exec")
				log.Printf("      Command: %v", probe.Exec.Command)
			}

			// Show timing configuration
			log.Printf("      Initial Delay: %ds", probe.InitialDelaySeconds)
			log.Printf("      Period: %ds", probe.PeriodSeconds)
			log.Printf("      Timeout: %ds", probe.TimeoutSeconds)
			log.Printf("      Success Threshold: %d", probe.SuccessThreshold)
			log.Printf("      Failure Threshold: %d", probe.FailureThreshold)

			return
		}
	}
}

// capturePodEvents retrieves and displays recent events for a pod.
func capturePodEvents(ctx context.Context, namespace, podName string) {
	events := &corev1.EventList{}
	if err := globalDebugClient.List(ctx, events, client.InNamespace(namespace)); err != nil {
		log.Printf("Failed to list events: %v", err)
		return
	}

	// Filter events for this pod
	var podEvents []corev1.Event
	for _, event := range events.Items {
		if event.InvolvedObject.Name == podName && event.InvolvedObject.Kind == "Pod" {
			podEvents = append(podEvents, event)
		}
	}

	if len(podEvents) == 0 {
		log.Printf("Pod Events: No events found")
		return
	}

	// Sort by timestamp (newest first)
	sort.Slice(podEvents, func(i, j int) bool {
		return podEvents[i].LastTimestamp.After(podEvents[j].LastTimestamp.Time)
	})

	log.Printf("Pod Events (most recent 10):")
	maxEvents := 10
	if len(podEvents) < maxEvents {
		maxEvents = len(podEvents)
	}

	for i := range maxEvents {
		event := podEvents[i]
		eventAge := time.Since(event.LastTimestamp.Time).Round(time.Second)
		eventType := "Normal"
		if event.Type == "Warning" {
			eventType = "WARNING"
		}
		log.Printf("  [%s ago] %s: %s - %s",
			eventAge, eventType, event.Reason, event.Message)
	}
}

// captureResourceInfo displays resource requests, limits, and actual usage.
func captureResourceInfo(pod *corev1.Pod) {
	log.Printf("Resource Requests and Limits:")

	for _, container := range pod.Spec.Containers {
		log.Printf("  Container: %s", container.Name)

		// Show CPU requests/limits
		if cpuReq := container.Resources.Requests[corev1.ResourceCPU]; !cpuReq.IsZero() {
			log.Printf("    CPU Request: %s", cpuReq.String())
		}
		if cpuLimit := container.Resources.Limits[corev1.ResourceCPU]; !cpuLimit.IsZero() {
			log.Printf("    CPU Limit: %s", cpuLimit.String())
		}

		// Show memory requests/limits
		if memReq := container.Resources.Requests[corev1.ResourceMemory]; !memReq.IsZero() {
			log.Printf("    Memory Request: %s", memReq.String())
		}
		if memLimit := container.Resources.Limits[corev1.ResourceMemory]; !memLimit.IsZero() {
			log.Printf("    Memory Limit: %s", memLimit.String())
		}
	}
}

// retrievePodLogsWithTail retrieves pod logs with a specific tail line count.
func retrievePodLogsWithTail(ctx context.Context, namespace, podName, containerName string, previous bool, tailLines int) (string, error) {
	// Get the Kubernetes REST config
	config, err := ctrlcfg.GetConfig()
	if err != nil {
		return "", fmt.Errorf("failed to get Kubernetes config: %w", err)
	}

	// Create a clientset for log access
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return "", fmt.Errorf("failed to create clientset: %w", err)
	}

	// Set log options
	tailLinesInt64 := int64(tailLines)
	podLogOpts := &corev1.PodLogOptions{
		Container: containerName,
		TailLines: &tailLinesInt64,
		Previous:  previous,
	}

	// Get logs request
	req := clientset.CoreV1().Pods(namespace).GetLogs(podName, podLogOpts)

	// Stream logs
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return "", fmt.Errorf("error opening log stream: %w", err)
	}
	defer func() {
		if closeErr := podLogs.Close(); closeErr != nil {
			log.Printf("Warning: failed to close log stream: %v", closeErr)
		}
	}()

	// Read logs
	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, podLogs)
	if err != nil {
		return "", fmt.Errorf("error reading logs: %w", err)
	}

	return buf.String(), nil
}

func getControllerDeploymentName() string {
	defaultControllerDeploymentName := controllerDeploymentODH
	dsci := getDSCI()
	if dsci == nil {
		return defaultControllerDeploymentName
	}
	platform, ok, err := unstructured.NestedString(dsci.Object, "status", "release", "name")
	if err != nil {
		log.Printf("Failed to get platform from DSCI: %v", err)
		return defaultControllerDeploymentName
	}
	if !ok {
		log.Printf("Failed to get platform from DSCI: platform not found")
		return defaultControllerDeploymentName
	}
	return getControllerDeploymentNameByPlatform(common.Platform(platform))
}
