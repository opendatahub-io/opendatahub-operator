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
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlcfg "sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/opendatahub-io/opendatahub-operator/pkg/clusterhealth"
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/failureclassifier"
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
	logTypeCurrent                    = "current"
	logTypePrevious                   = "previous"
	testManagerName                   = "manager"
	testMLflowPod                     = "mlflow-operator-controller-manager-0"
	testNamespace                     = "opendatahub"
	mlflowOperatorCRName              = "default-mlflowoperator"
	mlflowOperatorDeploymentName      = "mlflow-operator-controller-manager"
	mlflowOperatorControllerToggleEnv = "ENABLE_MLFLOW_OPERATOR_MODULE_CONTROLLER"
	applicationsNamespaceEnvName      = "APPLICATIONS_NAMESPACE"
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
	lastClassification atomic.Pointer[failureclassifier.FailureClassification]
	podLogsFetcher     = retrievePodLogs
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

// redactEvidence applies redactSensitiveInfo to each evidence string in a classification.
func redactEvidence(fc *failureclassifier.FailureClassification) {
	for i, e := range fc.Evidence {
		fc.Evidence[i] = redactSensitiveInfo(e)
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cfg := clusterhealth.Config{
		Client: globalDebugClient,
		Operator: clusterhealth.OperatorConfig{
			Namespace: testOpts.operatorNamespace,
			Name:      getControllerDeploymentName(ctx),
		},
		Namespaces: clusterhealth.NamespaceConfig{
			Apps:  testOpts.appsNamespace,
			Extra: []string{"kube-system"},
		},
		DSCI: types.NamespacedName{Name: dsciInstanceName},
		DSC:  types.NamespacedName{Name: dscInstanceName},
	}

	report, err := clusterhealth.Run(ctx, cfg)
	if err != nil {
		log.Printf("ERROR: Failed to collect diagnostics: %v", err)
		fc := failureclassifier.Classify(nil)
		fc.Evidence = append(fc.Evidence, fmt.Sprintf("clusterhealth.Run error: %v", err))
		redactEvidence(&fc)
		failureclassifier.EmitClassification(fc, testName)
		lastClassification.Store(&fc)
		return
	}

	logReport(report)
	logMLflowOperatorDiagnostics(ctx)

	fc := failureclassifier.Classify(report)
	redactEvidence(&fc)
	failureclassifier.EmitClassification(fc, testName)
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
		if len(report.Pods.Data.Data) == 0 {
			return
		}
		log.Printf("Continuing with partial pod data to collect logs from problematic containers")
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

func logMLflowOperatorDiagnostics(ctx context.Context) {
	log.Printf("=== MLFLOW OPERATOR DIAGNOSTICS ===")
	logMLflowOperatorCRSection(ctx)
	logMLflowOperatorDeploymentSection(ctx)
	logMLflowOperatorPodSection(ctx)
}

func logMLflowOperatorCRSection(ctx context.Context) {
	log.Printf("--- MLflowOperator CR ---")
	if globalDebugClient == nil {
		log.Printf("No Kubernetes client available for MLflowOperator CR diagnostics")
		return
	}

	module := &unstructured.Unstructured{}
	module.SetGroupVersionKind(gvk.MLflowOperator)

	err := globalDebugClient.Get(ctx, types.NamespacedName{Name: mlflowOperatorCRName}, module)
	switch {
	case k8serr.IsNotFound(err):
		log.Printf("MLflowOperator %q not found", mlflowOperatorCRName)
		return
	case err != nil:
		log.Printf("Failed to get MLflowOperator %q: %v", mlflowOperatorCRName, err)
		return
	}

	gatewayName, _, _ := unstructured.NestedString(module.Object, "spec", "gatewayName")
	sectionTitle, _, _ := unstructured.NestedString(module.Object, "spec", "sectionTitle")
	gatewayDomain, _, _ := unstructured.NestedString(module.Object, "spec", "gateway", "domain")
	observedGeneration, _, _ := unstructured.NestedInt64(module.Object, "status", "observedGeneration")
	phase, _, _ := unstructured.NestedString(module.Object, "status", "phase")

	log.Printf(
		"MLflowOperator %q: generation=%d observedGeneration=%d phase=%q",
		module.GetName(),
		module.GetGeneration(),
		observedGeneration,
		phase,
	)
	log.Printf(
		"  spec.gatewayName=%q spec.sectionTitle=%q spec.gateway.domain=%q",
		gatewayName,
		sectionTitle,
		gatewayDomain,
	)

	rawConditions, found, err := unstructured.NestedSlice(module.Object, "status", "conditions")
	if err != nil {
		log.Printf("  Failed to read MLflowOperator conditions: %v", err)
		return
	}
	if !found || len(rawConditions) == 0 {
		log.Printf("  No MLflowOperator status conditions found")
		return
	}

	for _, rawCondition := range rawConditions {
		conditionMap, ok := rawCondition.(map[string]any)
		if !ok {
			continue
		}
		conditionType, _ := conditionMap["type"].(string)
		conditionStatus, _ := conditionMap["status"].(string)
		conditionReason, _ := conditionMap["reason"].(string)
		conditionMessage, _ := conditionMap["message"].(string)
		log.Printf(
			"  %s: %s - %s",
			conditionType,
			conditionStatus,
			redactSensitiveInfo(strings.TrimSpace(strings.Join([]string{conditionReason, conditionMessage}, " "))),
		)
	}
}

func logMLflowOperatorDeploymentSection(ctx context.Context) {
	log.Printf("--- MLflow Operator Deployment ---")
	if globalDebugClient == nil {
		log.Printf("No Kubernetes client available for MLflow operator Deployment diagnostics")
		return
	}

	namespace := mlflowOperatorNamespace()
	deployment := &appsv1.Deployment{}
	err := globalDebugClient.Get(ctx, types.NamespacedName{
		Namespace: namespace,
		Name:      mlflowOperatorDeploymentName,
	}, deployment)
	switch {
	case k8serr.IsNotFound(err):
		log.Printf("Deployment %s/%s not found", namespace, mlflowOperatorDeploymentName)
		return
	case err != nil:
		log.Printf("Failed to get Deployment %s/%s: %v", namespace, mlflowOperatorDeploymentName, err)
		return
	}

	replicas := int32(1)
	if deployment.Spec.Replicas != nil {
		replicas = *deployment.Spec.Replicas
	}

	log.Printf(
		"Deployment %s/%s: generation=%d observedGeneration=%d ready=%d/%d unavailable=%d",
		namespace,
		deployment.Name,
		deployment.Generation,
		deployment.Status.ObservedGeneration,
		deployment.Status.ReadyReplicas,
		replicas,
		deployment.Status.UnavailableReplicas,
	)
	for _, condition := range deployment.Status.Conditions {
		log.Printf("  %s: %s - %s", condition.Type, condition.Status, redactSensitiveInfo(condition.Message))
	}

	managerContainer := findNamedDeploymentContainer(deployment.Spec.Template.Spec.Containers, testManagerName)
	if managerContainer == nil {
		log.Printf("  Manager container %q not found on Deployment %s/%s", testManagerName, namespace, mlflowOperatorDeploymentName)
		return
	}

	log.Printf("  manager image=%q", managerContainer.Image)
	log.Printf(
		"  env %s=%s",
		mlflowOperatorControllerToggleEnv,
		formatEnvVarValue(findNamedEnvVar(managerContainer.Env, mlflowOperatorControllerToggleEnv)),
	)
	log.Printf(
		"  env %s=%s",
		applicationsNamespaceEnvName,
		formatEnvVarValue(findNamedEnvVar(managerContainer.Env, applicationsNamespaceEnvName)),
	)
}

func logMLflowOperatorPodSection(ctx context.Context) {
	log.Printf("--- MLflow Operator Pods ---")
	if globalDebugClient == nil {
		log.Printf("No Kubernetes client available for MLflow operator pod diagnostics")
		return
	}

	namespace := mlflowOperatorNamespace()
	podList := &corev1.PodList{}
	if err := globalDebugClient.List(ctx, podList, client.InNamespace(namespace)); err != nil {
		log.Printf("Failed to list pods in namespace %s: %v", namespace, err)
		return
	}

	var mlflowPods []corev1.Pod
	for _, pod := range podList.Items {
		if strings.HasPrefix(pod.Name, mlflowOperatorDeploymentName+"-") {
			mlflowPods = append(mlflowPods, pod)
		}
	}

	if len(mlflowPods) == 0 {
		log.Printf("No MLflow operator pods found in namespace %s", namespace)
		return
	}

	sort.Slice(mlflowPods, func(i, j int) bool {
		return mlflowPods[i].Name < mlflowPods[j].Name
	})

	for _, pod := range mlflowPods {
		log.Printf("Pod %s: phase=%s", pod.Name, pod.Status.Phase)
		for _, containerStatus := range pod.Status.ContainerStatuses {
			waitingReason := ""
			if containerStatus.State.Waiting != nil {
				waitingReason = containerStatus.State.Waiting.Reason
			}

			terminatedState := ""
			if containerStatus.State.Terminated != nil {
				terminatedState = fmt.Sprintf("%s (exit %d)", containerStatus.State.Terminated.Reason, containerStatus.State.Terminated.ExitCode)
			}

			log.Printf(
				"  Container %s: ready=%v restarts=%d waiting=%q terminated=%q",
				containerStatus.Name,
				containerStatus.Ready,
				containerStatus.RestartCount,
				waitingReason,
				terminatedState,
			)

			if containerStatus.Name != testManagerName || containerStatus.Ready {
				continue
			}

			// Fetch current logs even without restarts so we capture startup failures
			// where the container never becomes Ready but has not crashed yet.
			logContainerLogs(pod.Name, containerStatus.Name, pod.Namespace, logTypeCurrent)
			if containerStatus.RestartCount > 0 {
				logContainerLogs(pod.Name, containerStatus.Name, pod.Namespace, logTypePrevious)
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

	logs, err := podLogsFetcher(namespace, podName, containerName, logType == "previous")
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

func mlflowOperatorNamespace() string {
	if testOpts.appsNamespace != "" {
		return testOpts.appsNamespace
	}
	return testNamespace
}

func findNamedDeploymentContainer(containers []corev1.Container, targetName string) *corev1.Container {
	for i := range containers {
		if containers[i].Name == targetName {
			return &containers[i]
		}
	}
	return nil
}

func findNamedEnvVar(envVars []corev1.EnvVar, targetName string) *corev1.EnvVar {
	for i := range envVars {
		if envVars[i].Name == targetName {
			return &envVars[i]
		}
	}
	return nil
}

func formatEnvVarValue(envVar *corev1.EnvVar) string {
	if envVar == nil {
		return "<unset>"
	}
	if envVar.ValueFrom != nil {
		return "<valueFrom>"
	}
	if envVar.Value == "" {
		return "<empty>"
	}
	return fmt.Sprintf("%q", redactSensitiveInfo(envVar.Value))
}

func TestLogPodsSectionContinuesWithPartialData(t *testing.T) {
	var buf bytes.Buffer
	originalWriter := log.Writer()
	originalFetcher := podLogsFetcher
	log.SetOutput(&buf)
	defer log.SetOutput(originalWriter)

	podLogsFetcher = func(namespace, podName, containerName string, previous bool) (string, error) {
		if namespace != testNamespace || podName != testMLflowPod || containerName != testManagerName || !previous {
			t.Fatalf("unexpected log request namespace=%q pod=%q container=%q previous=%v", namespace, podName, containerName, previous)
		}
		return "line-1\nline-2", nil
	}
	defer func() {
		podLogsFetcher = originalFetcher
	}()

	report := &clusterhealth.Report{
		Pods: clusterhealth.SectionResult[clusterhealth.PodsSection]{
			Error: testNamespace + "/" + testMLflowPod + ": container manager not ready",
			Data: clusterhealth.PodsSection{
				ByNamespace: map[string][]clusterhealth.PodInfo{
					testNamespace: {{
						Namespace: testNamespace,
						Name:      testMLflowPod,
						Phase:     string(corev1.PodRunning),
						Containers: []clusterhealth.ContainerInfo{{
							Name:         testManagerName,
							Ready:        false,
							RestartCount: 1,
							Waiting:      CrashLoopBackOff,
						}},
					}},
				},
				Data: []corev1.Pod{{
					ObjectMeta: metav1.ObjectMeta{
						Name:      testMLflowPod,
						Namespace: testNamespace,
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{{
							Name:         testManagerName,
							Ready:        false,
							RestartCount: 1,
							State: corev1.ContainerState{
								Waiting: &corev1.ContainerStateWaiting{Reason: CrashLoopBackOff},
							},
						}},
					},
				}},
			},
		},
	}

	logPodsSection(report)

	output := buf.String()
	if !strings.Contains(output, "Failed to collect pod data") {
		t.Fatalf("expected pod section error to be logged, got: %s", output)
	}
	if !strings.Contains(output, "Continuing with partial pod data to collect logs") {
		t.Fatalf("expected partial-data continuation message, got: %s", output)
	}
	if !strings.Contains(output, "=== LOGS (PREVIOUS) for container manager ===") {
		t.Fatalf("expected container logs to be emitted, got: %s", output)
	}
	if !strings.Contains(output, "line-2") {
		t.Fatalf("expected fetched pod log content in output, got: %s", output)
	}
}

func TestFormatEnvVarValue(t *testing.T) {
	t.Run("unset", func(t *testing.T) {
		if got := formatEnvVarValue(nil); got != "<unset>" {
			t.Fatalf("expected <unset>, got %s", got)
		}
	})

	t.Run("literal", func(t *testing.T) {
		got := formatEnvVarValue(&corev1.EnvVar{Name: mlflowOperatorControllerToggleEnv, Value: "true"})
		if got != `"true"` {
			t.Fatalf("expected quoted literal, got %s", got)
		}
	})

	t.Run("valueFrom", func(t *testing.T) {
		got := formatEnvVarValue(&corev1.EnvVar{
			Name: applicationsNamespaceEnvName,
			ValueFrom: &corev1.EnvVarSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{},
			},
		})
		if got != "<valueFrom>" {
			t.Fatalf("expected <valueFrom>, got %s", got)
		}
	})
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

	// Stream logs with a bounded timeout to prevent indefinite hangs
	logCtx, logCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer logCancel()

	podLogs, err := req.Stream(logCtx)
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

func getDSCI(ctx context.Context) *unstructured.Unstructured {
	dsci := &unstructured.Unstructured{}
	dsci.SetGroupVersionKind(gvk.DSCInitialization)
	err := globalDebugClient.Get(ctx, types.NamespacedName{Name: dsciInstanceName}, dsci)
	if err != nil && !k8serr.IsNotFound(err) {
		log.Printf("Failed to get DSCI: %v", err)
		return nil
	}

	if k8serr.IsNotFound(err) {
		log.Printf("No DSCI instance found")
		return nil
	}
	return dsci
}

func getControllerDeploymentName(ctx context.Context) string {
	// First try to detect platform from the DSCI status.
	dsci := getDSCI(ctx)
	if dsci != nil {
		platform, ok, err := unstructured.NestedString(dsci.Object, "status", "release", "name")
		if err == nil && ok {
			return getControllerDeploymentNameByPlatform(common.Platform(platform))
		}
		log.Printf("Failed to get platform from DSCI (err=%v, ok=%v), falling back to deployment probe", err, ok)
	}

	// Fall back to discovering the operator deployment in the namespace.
	// This is needed during preflight health checks when the DSCI may not exist yet.
	if globalDebugClient != nil {
		deploy, err := clusterhealth.FindOperatorDeploymentInNamespace(ctx, globalDebugClient, testOpts.operatorNamespace)
		if err == nil && deploy != nil {
			return deploy.Name
		}
		log.Printf("Failed to discover operator deployment in namespace %s: %v", testOpts.operatorNamespace, err)
	}

	return controllerDeploymentODH
}
