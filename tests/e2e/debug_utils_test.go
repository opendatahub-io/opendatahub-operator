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
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
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

	// Log type constants.
	logTypeCurrent  = "current"
	logTypePrevious = "previous"
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
// Uses dynamic namespace discovery to find pods in the correct namespace.
func getOperatorPods() (*corev1.PodList, error) {
	// Discover platform and deployment info
	platform := fetchPlatformFromDSCI()
	deploymentName := getControllerDeploymentName(platform)
	operatorNamespace := discoverOperatorNamespace(deploymentName)

	// Try OpenDataHub selector first
	odhPods := &corev1.PodList{}
	err := globalDebugClient.List(context.TODO(), odhPods,
		client.InNamespace(operatorNamespace),
		client.MatchingLabels{"control-plane": "controller-manager"})

	if err != nil {
		return nil, err
	}

	// Try RHODS selector
	rhoadsPods := &corev1.PodList{}
	err = globalDebugClient.List(context.TODO(), rhoadsPods,
		client.InNamespace(operatorNamespace),
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

				// Check pod conditions
				for _, condition := range pod.Status.Conditions {
					if condition.Status != corev1.ConditionTrue {
						log.Printf("    %s: %s - %s", condition.Type, condition.Status, condition.Message)
					}
				}

				// Show container states and get logs for problematic containers
				logContainerStates(pod.Status.ContainerStatuses, "    ")
				logProblematicContainerLogs(pod, ns)
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

		// Show pod conditions
		for _, condition := range pod.Status.Conditions {
			if condition.Status != corev1.ConditionTrue {
				log.Printf("      %s: %s - %s", condition.Type, condition.Status, condition.Message)
			}
		}

		// Show container states (waiting/terminated containers)
		logContainerStates(pod.Status.ContainerStatuses, "      ")
		logContainerStates(pod.Status.InitContainerStatuses, "      ")

		// Get recent logs for problematic containers
		logProblematicContainerLogs(pod, namespace)
	}
}

// logContainerStates logs waiting or terminated container states.
func logContainerStates(containerStatuses []corev1.ContainerStatus, indent string) {
	for _, containerStatus := range containerStatuses {
		restartInfo := ""
		if containerStatus.RestartCount > 0 {
			restartInfo = fmt.Sprintf(" (restarts: %d)", containerStatus.RestartCount)
		}

		if containerStatus.State.Waiting != nil {
			log.Printf("%sContainer %s waiting: %s - %s%s",
				indent,
				containerStatus.Name,
				containerStatus.State.Waiting.Reason,
				containerStatus.State.Waiting.Message,
				restartInfo)
		}
		if containerStatus.State.Terminated != nil {
			log.Printf("%sContainer %s terminated: %s - %s (exit: %d)%s",
				indent,
				containerStatus.Name,
				containerStatus.State.Terminated.Reason,
				containerStatus.State.Terminated.Message,
				containerStatus.State.Terminated.ExitCode,
				restartInfo)
		}
		if !containerStatus.Ready && containerStatus.State.Running != nil {
			log.Printf("%sContainer %s running but not ready%s",
				indent,
				containerStatus.Name,
				restartInfo)
		}
	}
}

// logProblematicContainerLogs retrieves recent logs for containers that are failing or restarting.
func logProblematicContainerLogs(pod corev1.Pod, namespace string) {
	for _, containerStatus := range pod.Status.ContainerStatuses {
		logType := determineLogType(containerStatus)
		if logType == "" {
			continue
		}

		getPodContainerLogs(pod.Name, containerStatus.Name, namespace, logType)
	}
}

// determineLogType returns the type of logs to retrieve or empty string if no logs needed.
func determineLogType(containerStatus corev1.ContainerStatus) string {
	// Get logs from previous crashed container (when waiting after restart)
	if containerStatus.State.Waiting != nil && containerStatus.RestartCount > 0 {
		return logTypePrevious
	}

	// Get current logs for terminated containers
	if containerStatus.State.Terminated != nil {
		return logTypeCurrent
	}

	// Get current logs for not-ready containers with restarts
	if !containerStatus.Ready && containerStatus.RestartCount > 0 {
		return logTypeCurrent
	}

	// Get current logs for containers in problematic waiting states (even without restarts)
	if containerStatus.State.Waiting != nil {
		reason := containerStatus.State.Waiting.Reason
		if reason == CrashLoopBackOff || reason == ImagePullBackOff ||
			reason == ErrImagePull || reason == InvalidImageName ||
			reason == CreateContainerConfigError || reason == CreateContainerError {
			return logTypeCurrent
		}
	}

	// No logs needed
	return ""
}

// getPodContainerLogs retrieves and displays the last few lines of container logs.
func getPodContainerLogs(podName, containerName, namespace, logType string) {
	log.Printf("        === LOGS (%s) for container %s ===", strings.ToUpper(logType), containerName)

	if globalDebugClient == nil {
		log.Printf("        No debug client available for log retrieval")
		log.Printf("        === END LOGS ===")
		return
	}

	logs, err := retrievePodLogs(namespace, podName, containerName, logType == "previous")
	if err != nil {
		log.Printf("        Failed to retrieve logs: %v", err)
		return
	}

	if logs == "" {
		log.Printf("        No logs available")
		return
	}

	// Redact sensitive information before printing
	redactedLogs := redactSensitiveInfo(logs)

	// Print the last 10 lines of logs
	logLines := strings.Split(strings.TrimSpace(redactedLogs), "\n")
	maxLines := 10
	startIdx := 0
	if len(logLines) > maxLines {
		startIdx = len(logLines) - maxLines
	}

	for i := startIdx; i < len(logLines); i++ {
		if strings.TrimSpace(logLines[i]) != "" {
			log.Printf("        %s", logLines[i])
		}
	}

	log.Printf("        === END LOGS ===")
}

// retrievePodLogs gets the actual logs from a pod container using client-go.
func retrievePodLogs(namespace, podName, containerName string, previous bool) (string, error) {
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
	tailLines := int64(10)
	podLogOpts := &corev1.PodLogOptions{
		Container: containerName,
		TailLines: &tailLines,
		Previous:  previous,
	}

	// Get logs request
	req := clientset.CoreV1().Pods(namespace).GetLogs(podName, podLogOpts)

	// Stream logs
	podLogs, err := req.Stream(context.TODO())
	if err != nil {
		return "", fmt.Errorf("error opening log stream: %w", err)
	}
	defer func() {
		if closeErr := podLogs.Close(); closeErr != nil {
			log.Printf("        Warning: failed to close log stream: %v", closeErr)
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

// fetchPlatformFromDSCI fetches platform directly from DSCInitialization resource.
func fetchPlatformFromDSCI() common.Platform {
	// Fetch DSCI to get platform info
	dsci := &dsciv2.DSCInitialization{}
	err := globalDebugClient.Get(context.TODO(), types.NamespacedName{Name: dsciInstanceName}, dsci)
	if err != nil {
		log.Printf("Warning: Could not fetch DSCI for platform detection: %v", err)
		return cluster.OpenDataHub // fallback to ODH
	}

	// Get platform from DSCI status
	if dsci.Status.Release.Name != "" {
		return dsci.Status.Release.Name
	}

	log.Printf("Warning: DSCI release name is empty, falling back to OpenDataHub")
	return cluster.OpenDataHub
}

// discoverOperatorNamespace dynamically finds the actual namespace containing the operator deployment.
func discoverOperatorNamespace(deploymentName string) string {
	// Candidate namespaces in order of likelihood
	candidateNamespaces := []string{
		"openshift-operators",         // Most common for OLM operators
		"redhat-ods-operator",         // RHOAI fallback from cluster config
		"opendatahub-operator-system", // ODH default from test config
		testOpts.operatorNamespace,    // Current test configuration
	}

	for _, namespace := range candidateNamespaces {
		deployment := &appsv1.Deployment{}
		err := globalDebugClient.Get(context.TODO(),
			types.NamespacedName{Name: deploymentName, Namespace: namespace},
			deployment)

		if err == nil {
			log.Printf("Found operator deployment '%s' in namespace '%s'", deploymentName, namespace)
			return namespace
		}
	}

	log.Printf("Warning: Could not find deployment '%s' in any candidate namespace, using fallback", deploymentName)
	return testOpts.operatorNamespace // fallback to test configuration
}

// getControllerDeploymentName returns deployment name based on platform.
func getControllerDeploymentName(platform common.Platform) string {
	switch platform {
	case cluster.SelfManagedRhoai, cluster.ManagedRhoai:
		return controllerDeploymentRhoai
	case cluster.OpenDataHub:
		return controllerDeploymentODH
	default:
		return controllerDeploymentODH
	}
}

// debugOperatorStatus checks the OpenDataHub operator deployment and pods.
func debugOperatorStatus() {
	log.Printf("=== OPERATOR STATUS ===")

	// Fetch platform directly from DSCI to get deployment name
	platform := fetchPlatformFromDSCI()
	deploymentName := getControllerDeploymentName(platform)
	log.Printf("Detected platform: %s, using deployment: %s", platform, deploymentName)

	// Dynamically discover the actual namespace containing the deployment
	operatorNamespace := discoverOperatorNamespace(deploymentName)
	log.Printf("Using namespace: %s", operatorNamespace)

	// Check main operator deployment
	operatorDeploy := &appsv1.Deployment{}
	err := globalDebugClient.Get(context.TODO(),
		types.NamespacedName{Name: deploymentName, Namespace: operatorNamespace},
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

// debugDSCIStatus checks DataScienceClusterInitialization and DataScienceCluster status.
func debugDSCIStatus() {
	log.Printf("=== DSCI/DSC STATUS ===")

	// Check DSCI (prerequisite for DSC)
	dsci := &unstructured.Unstructured{}
	dsci.SetGroupVersionKind(gvk.DSCInitialization)
	err := globalDebugClient.Get(context.TODO(), types.NamespacedName{Name: dsciInstanceName}, dsci)
	if err != nil && !errors.IsNotFound(err) {
		log.Printf("Failed to get DSCI: %v", err)
		return
	}

	if errors.IsNotFound(err) {
		log.Printf("No DSCI instance found")
		return
	}

	log.Printf("DSCI %s:", dsci.GetName())
	logUnhealthyConditions(dsci.Object)

	// Check DSC (singleton resource - depends on DSCI)
	dsc := &unstructured.Unstructured{}
	dsc.SetGroupVersionKind(gvk.DataScienceCluster)
	err = globalDebugClient.Get(context.TODO(), types.NamespacedName{Name: dscInstanceName}, dsc)
	if err != nil && !errors.IsNotFound(err) {
		log.Printf("Failed to get DSC: %v", err)
		return
	}

	if errors.IsNotFound(err) {
		log.Printf("No DSC instance found")
		return
	}

	log.Printf("DSC %s:", dsc.GetName())
	logUnhealthyConditions(dsc.Object)
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
