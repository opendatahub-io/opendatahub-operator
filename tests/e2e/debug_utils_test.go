package e2e_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlcfg "sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/clusterhealth"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/e2e/pkg/classifier"
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
	// lastClassification stores the most recent failure classification for circuit breaker consumption.
	lastClassification atomic.Pointer[classifier.FailureClassification]
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
		runDiagnosticsOnce("panic", "panic") // key = "panic"
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
	runDiagnosticsOnce("test:"+testName, testName) // key = "test:<name>"
	log.Printf("Diagnostics complete for failed test: %s", testName)
}

// runDiagnosticsOnce ensures diagnostics run once per provided key (e.g., "panic", "test:<name>").
func runDiagnosticsOnce(key string, testName string) {
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

	runDiagnosticsAndClassify(testName)
}

// runDiagnosticsAndClassify collects cluster health data via clusterhealth.Run(),
// logs the report for CI visibility, and classifies the failure.
func runDiagnosticsAndClassify(testName string) {
	cfg := clusterhealth.Config{
		Client: globalDebugClient,
		Operator: clusterhealth.OperatorConfig{
			Namespace: testOpts.operatorNamespace,
			Name:      getControllerDeploymentName(),
		},
		Namespaces: clusterhealth.NamespaceConfig{
			Apps:  testOpts.appsNamespace,
			Extra: []string{"kube-system"},
		},
		DSCI: types.NamespacedName{Name: dsciInstanceName},
		DSC:  types.NamespacedName{Name: dscInstanceName},
	}

	report, err := clusterhealth.Run(context.TODO(), cfg)
	if err != nil {
		log.Printf("ERROR: Failed to collect diagnostics: %v", err)
		return
	}

	logReport(report)

	fc := classifier.Classify(report)
	classifier.EmitClassification(fc, testName)
	lastClassification.Store(&fc)
}

// logReport walks the clusterhealth Report sections and produces human-readable
// log output preserving the existing CI log format.
func logReport(report *clusterhealth.Report) {
	logNodesSection(report)
	logDeploymentsSection(report)
	logPodsSection(report)
	logOperatorSection(report)
	logCRConditionsSection("DSCI", report.DSCI)
	logCRConditionsSection("DSC", report.DSC)
	logEventsSection(report)
	logQuotasSection(report)
}

func logNodesSection(report *clusterhealth.Report) {
	log.Printf("=== CLUSTER STATE ===")
	if report.Nodes.Error != "" {
		log.Printf("Failed to collect node data: %s", report.Nodes.Error)
		return
	}
	if len(report.Nodes.Data.Nodes) == 0 {
		log.Printf("No nodes found in cluster")
		return
	}
	for _, node := range report.Nodes.Data.Nodes {
		log.Printf("Node: %s", node.Name)
		for _, condition := range node.Conditions {
			log.Printf("  Condition %s: %s - %s", condition.Type, condition.Status, redactSensitiveInfo(condition.Message))
		}
		if node.UnhealthyReason != "" {
			log.Printf("  UNHEALTHY: %s", redactSensitiveInfo(node.UnhealthyReason))
		}
		if node.Allocatable != "" || node.Capacity != "" {
			log.Printf("  Resources: %s allocatable (of %s capacity)", node.Allocatable, node.Capacity)
		}
	}
}

func logDeploymentsSection(report *clusterhealth.Report) {
	log.Printf("=== DEPLOYMENTS ===")
	if report.Deployments.Error != "" {
		log.Printf("Failed to collect deployment data: %s", report.Deployments.Error)
		return
	}
	for ns, deploys := range report.Deployments.Data.ByNamespace {
		log.Printf("Namespace: %s", ns)
		for _, d := range deploys {
			status := "READY"
			if d.Ready < d.Replicas {
				status = "NOT READY"
			}
			log.Printf("  Deployment %s: %d/%d (%s)", d.Name, d.Ready, d.Replicas, status)
			if d.Ready < d.Replicas {
				for _, condition := range d.Conditions {
					log.Printf("    %s: %s - %s", condition.Type, condition.Status, redactSensitiveInfo(condition.Message))
				}
			}
		}
	}
}

func logPodsSection(report *clusterhealth.Report) {
	log.Printf("=== PODS ===")
	if report.Pods.Error != "" {
		log.Printf("Failed to collect pod data: %s", report.Pods.Error)
		return
	}
	for ns, pods := range report.Pods.Data.ByNamespace {
		problemsFound := false
		for _, pod := range pods {
			if !podHasIssues(pod) {
				continue
			}
			if !problemsFound {
				log.Printf("Namespace: %s", ns)
				problemsFound = true
			}
			log.Printf("  Pod %s: %s", pod.Name, pod.Phase)
			for _, container := range pod.Containers {
				parts := []string{fmt.Sprintf("ready=%v", container.Ready)}
				if container.RestartCount > 0 {
					parts = append(parts, fmt.Sprintf("restarts=%d", container.RestartCount))
				}
				if container.Waiting != "" {
					parts = append(parts, fmt.Sprintf("waiting: %s", redactSensitiveInfo(container.Waiting)))
				}
				if container.Terminated != "" {
					parts = append(parts, fmt.Sprintf("terminated: %s", redactSensitiveInfo(container.Terminated)))
				}
				if !container.Ready || container.Waiting != "" || container.Terminated != "" || container.RestartCount > 0 {
					log.Printf("    Container %s: %s", container.Name, strings.Join(parts, ", "))
				}
			}
		}
		if !problemsFound {
			log.Printf("Namespace %s: no problematic pods found", ns)
		}
	}

	// Retrieve actual container logs for problematic pods using raw Pod objects
	if len(report.Pods.Data.Data) > 0 {
		logProblematicContainerLogs(report.Pods.Data.Data)
	}
}

// podHasIssues returns true if the pod is in a non-healthy state or has containers
// that are not ready, waiting, terminated, or restarting.
func podHasIssues(pod clusterhealth.PodInfo) bool {
	if pod.Phase != "Running" && pod.Phase != "Succeeded" {
		return true
	}
	for _, c := range pod.Containers {
		if !c.Ready || c.Waiting != "" || c.Terminated != "" || c.RestartCount > 0 {
			return true
		}
	}
	return false
}

func logOperatorSection(report *clusterhealth.Report) {
	log.Printf("=== OPERATOR STATUS ===")
	if report.Operator.Error != "" {
		log.Printf("Failed to collect operator data: %s", report.Operator.Error)
		return
	}
	if report.Operator.Data.Deployment != nil {
		d := report.Operator.Data.Deployment
		log.Printf("Operator deployment: %d/%d ready", d.Ready, d.Replicas)
		for _, condition := range d.Conditions {
			log.Printf("  %s: %s - %s", condition.Type, condition.Status, redactSensitiveInfo(condition.Message))
		}
	}
	for _, pod := range report.Operator.Data.Pods {
		if pod.Phase != "Running" {
			log.Printf("Operator pod %s: %s", pod.Name, pod.Phase)
			for _, container := range pod.Containers {
				if container.RestartCount > 0 {
					log.Printf("  Container %s restarted %d times", container.Name, container.RestartCount)
				}
			}
		}
	}
}

func logCRConditionsSection(name string, section clusterhealth.SectionResult[clusterhealth.CRConditionsSection]) {
	log.Printf("=== %s STATUS ===", name)
	if section.Error != "" {
		log.Printf("Failed to collect %s data: %s", name, section.Error)
		return
	}
	if section.Data.Name == "" {
		log.Printf("No %s instance found", name)
		return
	}
	log.Printf("%s %s:", name, section.Data.Name)
	for _, condition := range section.Data.Conditions {
		log.Printf("  %s: %s - %s", condition.Type, condition.Status, redactSensitiveInfo(condition.Message))
	}
}

func logEventsSection(report *clusterhealth.Report) {
	log.Printf("=== RECENT EVENTS ===")
	if report.Events.Error != "" {
		log.Printf("Failed to collect event data: %s", report.Events.Error)
		return
	}
	if len(report.Events.Data.Events) == 0 {
		log.Printf("No recent events found in monitored namespaces")
		return
	}
	for _, event := range report.Events.Data.Events {
		eventType := "INFO"
		if event.Type == "Warning" {
			eventType = "WARN"
		}
		log.Printf("  %s %s %s/%s: %s - %s",
			event.LastTime.Format("15:04:05"),
			eventType,
			event.Kind, event.Name,
			event.Reason, redactSensitiveInfo(event.Message))
	}
}

func logQuotasSection(report *clusterhealth.Report) {
	log.Printf("=== RESOURCE QUOTAS ===")
	if report.Quotas.Error != "" {
		log.Printf("Failed to collect quota data: %s", report.Quotas.Error)
		return
	}
	hasQuotas := false
	for ns, quotas := range report.Quotas.Data.ByNamespace {
		for _, q := range quotas {
			hasQuotas = true
			log.Printf("Namespace %s, ResourceQuota %s:", ns, q.Name)
			if len(q.Exceeded) > 0 {
				log.Printf("  QUOTA EXCEEDED: %v", q.Exceeded)
			} else {
				log.Printf("  No quota violations detected")
			}
			for resource, used := range q.Used {
				if hard, ok := q.Hard[resource]; ok {
					log.Printf("  %s: %s / %s", resource, used, hard)
				}
			}
		}
	}
	if !hasQuotas {
		log.Printf("No resource quotas found in monitored namespaces")
	}
}

// logProblematicContainerLogs retrieves recent logs for containers that are failing or restarting.
// It uses the raw Pod objects from the clusterhealth report to access container status details
// needed for log retrieval decisions.
func logProblematicContainerLogs(pods []corev1.Pod) {
	for _, pod := range pods {
		for _, cs := range pod.Status.ContainerStatuses {
			logType := determineLogType(cs)
			if logType == "" {
				continue
			}
			logContainerLogs(pod.Name, cs.Name, pod.Namespace, logType)
		}
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

// logContainerLogs retrieves and displays the last few lines of container logs.
func logContainerLogs(podName, containerName, namespace, logType string) {
	log.Printf("        === LOGS (%s) for container %s ===", strings.ToUpper(logType), containerName)

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

func getDSCI() *unstructured.Unstructured {
	dsci := &unstructured.Unstructured{}
	dsci.SetGroupVersionKind(gvk.DSCInitialization)
	err := globalDebugClient.Get(context.TODO(), types.NamespacedName{Name: dsciInstanceName}, dsci)
	if err != nil {
		log.Printf("Failed to get DSCI: %v", err)
		return nil
	}
	return dsci
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
