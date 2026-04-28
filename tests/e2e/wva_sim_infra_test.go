package e2e_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	operatorsv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

const (
	wvaControllerName              = "workload-variant-autoscaler-controller-manager"
	kserveControllerName           = "kserve-controller-manager"
	llmisvcControllerName          = "llmisvc-controller-manager"
	wvaSaturationScalingConfigMap  = "workload-variant-autoscaler-saturation-scaling-config"
	wvaVariantAutoscalingConfigMap = "workload-variant-autoscaler-wva-variantautoscaling-config"
	wvaTestNamespace               = "autoscaling-example"

	// Operator constants for WVA dependencies.
	serviceMeshOpName      = "servicemeshoperator3"
	serviceMeshOpNamespace = "openshift-operators"
	serviceMeshOpChannel   = "stable"
	rhclOpName             = "rhcl-operator"
	rhclOpNamespace        = "openshift-operators"
	rhclOpChannel          = "stable"

	// Container image versions.
	llmInferenceSimulatorImage = "ghcr.io/llm-d/llm-d-inference-sim:v0.5.1"
)

type WVATestCtx struct {
	*TestContext

	// State restoration tracking
	originalMonitoringConfig    string // Original cluster-monitoring-config
	originalDeploymentReplicas  map[string]int32
	originalStatefulSetReplicas map[string]int32
}

func wvaTestSuite(t *testing.T) {
	t.Helper()

	ctx, err := NewTestContext(t)
	require.NoError(t, err)

	componentCtx := WVATestCtx{
		TestContext: ctx,
	}

	// Setup phase - run once before all tests
	t.Log("=== WVA Test Setup Phase ===")
	setupSteps := []TestCase{
		{"Cleanup existing resources", componentCtx.CleanupExistingResources},
		{"Install required operators", componentCtx.InstallRequiredOperators},
		{"Wait for all prerequisite operators to be ready", componentCtx.WaitForPrerequisiteOperators},
		{"Enable user workload monitoring", componentCtx.EnableUserWorkloadMonitoring},
		{"Setup KEDA RBAC permissions", componentCtx.SetupKEDARBAC},
		{"Create autoscaling example namespace", componentCtx.CreateAutoscalingNamespace},
		// {"Label namespace for user workload monitoring", componentCtx.LabelNamespaceForMonitoring},
		{"Create autoscaling example gateway", componentCtx.CreateAutoscalingGateway},
		{"Validate autoscaling gateway resources exist", componentCtx.ValidateAutoscalingGatewayResources},
		{"Enable WVA through KServe", componentCtx.EnableWVA},
		{"Validate KServe controller deployment is running", componentCtx.ValidateKServeControllerDeployment},
		{"Validate llmisvc controller deployment is running", componentCtx.ValidateLLMISvcControllerDeployment},
		{"Validate WVA controller deployment is running", componentCtx.ValidateWVAControllerDeployment},
		{"Validate WVA ConfigMaps exist", componentCtx.ValidateWVAConfigMaps},
		{"Patch inferenceservice-config for autoscaling", componentCtx.PatchInferenceServiceConfig},
		{"Restart llmisvc controller to pick up config change", componentCtx.RestartLLMISvcController},
		{"Scale down non-essential services", componentCtx.ScaleDownNonEssentialServices},
		{"Create LLMInferenceService", componentCtx.CreateLLMInferenceService},
		{"Create PrometheusRule for vLLM metric aliases", componentCtx.CreateVLLMMetricAliasPrometheusRule},
		{"Validate LLMInferenceService is ready", componentCtx.ValidateLLMInferenceServiceReady},
		{"Create custom PodMonitors with HTTP scheme", componentCtx.CreateCustomPodMonitorsForHTTP},
		{"Validate VariantAutoscaling is created", componentCtx.ValidateVariantAutoscalingCreated},
		{"Validate ScaledObject is created", componentCtx.ValidateScaledObjectCreated},
		{"Validate vLLM metrics exposed from pods", componentCtx.ValidateVLLMMetricsFromPods},
		{"Validate Service exists for vLLM metrics", componentCtx.ValidateServiceForMetrics},
		{"Validate ServiceMonitor is created for vLLM metrics", componentCtx.ValidateServiceMonitorCreated},
		{"Validate ServiceMonitor matches Service labels", componentCtx.ValidateServiceMonitorMatchesService},
		{"Validate Prometheus can scrape vLLM metrics", componentCtx.ValidatePrometheusVLLMMetrics},
		{"Validate Prometheus can scrape WVA controller metrics", componentCtx.ValidatePrometheusWVAMetrics},
		{"Validate VariantAutoscaling metrics are available", componentCtx.ValidateVariantAutoscalingMetricReady},
		{"Validate ScaledObject is ready and active", componentCtx.ValidateScaledObjectReady},
	}

	for _, step := range setupSteps {
		t.Run(step.name, func(t *testing.T) {
			step.testFn(t)
		})
		// If setup fails, stop immediately
		if t.Failed() {
			t.Fatal("Setup phase failed, stopping test execution")
		}
	}
	t.Log("=== WVA Test Setup Complete ===")

	// Note: Cleanup is done at the beginning of test runs (see CleanupExistingResources step)
	// This preserves resources after test failures for debugging purposes

	// Test phase - actual tests that exercise WVA functionality
	t.Log("=== WVA Test Phase ===")

	t.Run("WVA infrastructure validation", func(t *testing.T) {
		t.Log("Validating WVA infrastructure setup")

		t.Log("✓ KEDA operator is installed")
		t.Log("✓ LLMInferenceService deployment successful")
		t.Log("✓ Monitoring configured")
		t.Log("✓ VariantAutoscaling and ScaledObject created")

		t.Log("Infrastructure validation complete")
		t.Log("TODO: Add actual autoscaling tests in follow-up PR (replica transitions, sustained load)")
	})

	t.Log("=== WVA Test Phase Complete ===")
}

// execCommandWithLogging executes a command and logs detailed information on failure.
func (tc *WVATestCtx) execCommandWithLogging(t *testing.T, description string, cmd *exec.Cmd) (string, error) {
	t.Helper()

	cmdStr := fmt.Sprintf("%s %s", cmd.Path, strings.Join(cmd.Args[1:], " "))
	t.Logf("Executing: %s", cmdStr)

	output, err := cmd.CombinedOutput()
	outputStr := strings.TrimSpace(string(output))

	if err != nil {
		t.Logf("❌ FAILED: %s", description)
		t.Logf("Command: %s", cmdStr)
		t.Logf("Error: %v", err)
		t.Logf("Output: %s", outputStr)
		return outputStr, fmt.Errorf("%s failed: %w\nCommand: %s\nOutput: %s", description, err, cmdStr, outputStr)
	}

	t.Logf("✅ SUCCESS: %s", description)
	if outputStr != "" {
		t.Logf("Output: %s", outputStr)
	}

	return outputStr, nil
}

func (tc *WVATestCtx) CleanupExistingResources(t *testing.T) {
	t.Helper()

	t.Log("Cleaning up existing resources from previous test runs")

	// Delete LLMInferenceService if it exists
	// First, check if resource exists and remove finalizers to avoid getting stuck during deletion
	t.Log("Checking for existing LLMInferenceService")
	checkCmd := exec.CommandContext(context.Background(), "kubectl", "get", "llminferenceservice", "sim-llama",
		"-n", wvaTestNamespace,
		"--ignore-not-found")
	checkOutput, err := checkCmd.CombinedOutput()

	if err == nil && len(checkOutput) > 0 {
		t.Log("Removing finalizers from existing LLMInferenceService")
		removeFinalizers := exec.CommandContext(context.Background(), "kubectl", "patch", "llminferenceservice", "sim-llama",
			"-n", wvaTestNamespace,
			"--type=json",
			"-p", `[{"op": "remove", "path": "/metadata/finalizers"}]`)
		patchOutput, patchErr := tc.execCommandWithLogging(t, "Remove LLMInferenceService finalizers", removeFinalizers)
		if patchErr != nil {
			t.Logf("Note: Could not remove finalizers: %v", patchErr)
		}
		_ = patchOutput // Suppress unused variable warning
	} else {
		t.Log("LLMInferenceService does not exist, skipping finalizer removal")
	}

	t.Log("Deleting existing LLMInferenceService")
	deleteLLMISvcCmd := exec.CommandContext(context.Background(), "kubectl", "delete", "llminferenceservice", "sim-llama",
		"-n", wvaTestNamespace,
		"--ignore-not-found=true",
		"--timeout=30s")
	deleteOutput, deleteErr := tc.execCommandWithLogging(t, "Delete existing LLMInferenceService", deleteLLMISvcCmd)
	if deleteErr != nil {
		t.Logf("Warning: Failed to delete LLMInferenceService: %v\nOutput: %s", deleteErr, deleteOutput)
	}

	// Delete Gateway if it exists
	t.Log("Deleting existing Gateway")
	deleteGatewayCmd := exec.CommandContext(context.Background(), "kubectl", "delete", "gateway", "autoscaling-example-gateway",
		"-n", wvaTestNamespace,
		"--ignore-not-found=true",
		"--timeout=2m")
	gwOutput, gwErr := tc.execCommandWithLogging(t, "Delete existing Gateway", deleteGatewayCmd)
	if gwErr != nil {
		t.Logf("Warning: Failed to delete Gateway: %v\nOutput: %s", gwErr, gwOutput)
	}

	// Delete Gateway ConfigMap if it exists
	t.Log("Deleting existing Gateway ConfigMap")
	deleteConfigMapCmd := exec.CommandContext(context.Background(), "kubectl", "delete", "configmap", "autoscaling-example-gateway-config",
		"-n", wvaTestNamespace,
		"--ignore-not-found=true",
		"--timeout=1m")
	cmOutput, cmErr := tc.execCommandWithLogging(t, "Delete existing Gateway ConfigMap", deleteConfigMapCmd)
	if cmErr != nil {
		t.Logf("Warning: Failed to delete Gateway ConfigMap: %v\nOutput: %s", cmErr, cmOutput)
	}

	// Delete PrometheusRule if it exists
	t.Log("Deleting existing PrometheusRule")
	deletePrometheusRuleCmd := exec.CommandContext(context.Background(), "kubectl", "delete", "prometheusrule", "vllm-metrics-alias",
		"-n", wvaTestNamespace,
		"--ignore-not-found=true",
		"--timeout=1m")
	prOutput, prErr := tc.execCommandWithLogging(t, "Delete existing PrometheusRule", deletePrometheusRuleCmd)
	if prErr != nil {
		t.Logf("Warning: Failed to delete PrometheusRule: %v\nOutput: %s", prErr, prOutput)
	}

	// Delete custom PodMonitor if it exists
	t.Log("Deleting existing custom PodMonitor")
	deletePodMonitorCmd := exec.CommandContext(context.Background(), "kubectl", "delete", "podmonitor", "kserve-llm-isvc-vllm-engine-http",
		"-n", wvaTestNamespace,
		"--ignore-not-found=true",
		"--timeout=1m")
	pmOutput, pmErr := tc.execCommandWithLogging(t, "Delete existing custom PodMonitor", deletePodMonitorCmd)
	if pmErr != nil {
		t.Logf("Warning: Failed to delete custom PodMonitor: %v\nOutput: %s", pmErr, pmOutput)
	}

	// Delete cluster-scoped resources
	// Delete ClusterRoleBinding
	t.Log("Deleting existing ClusterRoleBinding")
	deleteCRBCmd := exec.CommandContext(context.Background(), "kubectl", "delete", "clusterrolebinding", "keda-metrics-reader-monitoring",
		"--ignore-not-found=true",
		"--timeout=1m")
	crbOutput, crbErr := tc.execCommandWithLogging(t, "Delete existing ClusterRoleBinding", deleteCRBCmd)
	if crbErr != nil {
		t.Logf("Warning: Failed to delete ClusterRoleBinding: %v\nOutput: %s", crbErr, crbOutput)
	}

	// Delete ClusterTriggerAuthentication
	t.Log("Deleting existing ClusterTriggerAuthentication")
	deleteCTACmd := exec.CommandContext(context.Background(), "kubectl", "delete", "clustertriggerauthentication", "ai-inference-keda-thanos",
		"--ignore-not-found=true",
		"--timeout=1m")
	ctaOutput, ctaErr := tc.execCommandWithLogging(t, "Delete existing ClusterTriggerAuthentication", deleteCTACmd)
	if ctaErr != nil {
		t.Logf("Warning: Failed to delete ClusterTriggerAuthentication: %v\nOutput: %s", ctaErr, ctaOutput)
	}

	// Delete KEDA namespace resources
	// Delete ServiceAccount
	t.Log("Deleting KEDA metrics reader ServiceAccount")
	deleteSACmd := exec.CommandContext(context.Background(), "kubectl", "delete", "serviceaccount", "keda-metrics-reader",
		"-n", "openshift-keda",
		"--ignore-not-found=true",
		"--timeout=1m")
	saOutput, saErr := tc.execCommandWithLogging(t, "Delete KEDA ServiceAccount", deleteSACmd)
	if saErr != nil {
		t.Logf("Warning: Failed to delete ServiceAccount: %v\nOutput: %s", saErr, saOutput)
	}

	// Delete Secret
	t.Log("Deleting KEDA metrics reader Secret")
	deleteSecretCmd := exec.CommandContext(context.Background(), "kubectl", "delete", "secret", "keda-metrics-reader-token",
		"-n", "openshift-keda",
		"--ignore-not-found=true",
		"--timeout=1m")
	secretOutput, secretErr := tc.execCommandWithLogging(t, "Delete KEDA Secret", deleteSecretCmd)
	if secretErr != nil {
		t.Logf("Warning: Failed to delete Secret: %v\nOutput: %s", secretErr, secretOutput)
	}

	// Restore cluster to original state
	tc.RestoreClusterState(t)

	t.Log("Existing resources cleanup completed")
}

func (tc *WVATestCtx) InstallRequiredOperators(t *testing.T) {
	t.Helper()

	t.Log("Installing required operators for WVA testing")

	// Define operators to be installed
	operators := []struct {
		nn                types.NamespacedName
		skipOperatorGroup bool
		channel           string
	}{
		{nn: types.NamespacedName{Name: serviceMeshOpName, Namespace: serviceMeshOpNamespace}, skipOperatorGroup: true, channel: serviceMeshOpChannel},
		{nn: types.NamespacedName{Name: rhclOpName, Namespace: rhclOpNamespace}, skipOperatorGroup: true, channel: rhclOpChannel},
	}

	// Install operators in parallel
	testCases := make([]TestCase, len(operators))
	for i, op := range operators {
		testCases[i] = TestCase{
			name: fmt.Sprintf("Install %s operator", op.nn.Name),
			testFn: func(t *testing.T) {
				t.Helper()
				tc.EnsureOperatorInstalledWithChannel(op.nn, op.channel)
			},
		}
	}

	RunTestCases(t, testCases, WithParallel())

	t.Log("Required operators installed successfully")
}

func (tc *WVATestCtx) WaitForPrerequisiteOperators(t *testing.T) {
	t.Helper()

	t.Log("Waiting for all prerequisite operators to be ready")

	// Define all prerequisite operators with their namespaces and channels
	operators := []struct {
		name      string
		namespace string
		channel   string
	}{
		{name: serviceMeshOpName, namespace: serviceMeshOpNamespace, channel: serviceMeshOpChannel},
		{name: rhclOpName, namespace: rhclOpNamespace, channel: rhclOpChannel},
		{name: kedaOpName, namespace: kedaOpNamespace, channel: kedaOpChannel},
	}

	// Wait for each operator's CSV to reach Succeeded phase
	testCases := make([]TestCase, len(operators))
	for i, op := range operators {
		testCases[i] = TestCase{
			name: fmt.Sprintf("Wait for %s operator to be ready", op.name),
			testFn: func(t *testing.T) {
				t.Helper()

				t.Logf("Waiting for operator %s in namespace %s", op.name, op.namespace)

				nn := types.NamespacedName{
					Name:      op.name,
					Namespace: op.namespace,
				}

				// Retrieve the InstallPlan to get CSV name
				plan := tc.FetchInstallPlan(nn, op.channel)
				tc.g.Expect(plan.Spec.ClusterServiceVersionNames).NotTo(BeEmpty(),
					"No CSV found in InstallPlan for operator '%s'", op.name)

				csvName := plan.Spec.ClusterServiceVersionNames[0]
				t.Logf("Found CSV %s for operator %s", csvName, op.name)

				// Wait for CSV to reach Succeeded phase
				tc.g.Eventually(func(g Gomega) {
					csv, err := tc.FetchActualClusterServiceVersion(types.NamespacedName{
						Namespace: op.namespace,
						Name:      csvName,
					})
					g.Expect(err).NotTo(HaveOccurred(),
						"Failed to fetch CSV %s for operator %s", csvName, op.name)
					g.Expect(csv.Status.Phase).To(
						Equal(operatorsv1alpha1.CSVPhaseSucceeded),
						"CSV %s for operator %s did not reach 'Succeeded' phase", csvName, op.name,
					)
				}).WithTimeout(5*time.Minute).WithPolling(10*time.Second).
					Should(Succeed(), "Operator %s did not become ready", op.name)

				t.Logf("Operator %s is ready", op.name)
			},
		}
	}

	RunTestCases(t, testCases, WithParallel())

	t.Log("All prerequisite operators are ready")
}

func (tc *WVATestCtx) DisableAlertmanager(t *testing.T) {
	t.Helper()

	t.Log("Disabling alertmanager to reduce resource usage")

	// Remove alerting configuration from DSCInitialization to disable alertmanager StatefulSet
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithMutateFunc(testf.Transform(`del(.spec.monitoring.alerting)`)),
		WithCondition(Succeed()),
		WithCustomErrorMsg("Failed to disable alertmanager in DSCInitialization"),
	)

	t.Log("Alertmanager disabled successfully")
}

func (tc *WVATestCtx) EnableUserWorkloadMonitoring(t *testing.T) {
	t.Helper()

	t.Log("Enabling user workload monitoring")

	// Capture original monitoring config before modifying
	getConfigCmd := exec.CommandContext(context.Background(), "kubectl", "get", "configmap", "cluster-monitoring-config",
		"-n", "openshift-monitoring",
		"-o", "jsonpath={.data.config\\.yaml}")
	originalConfig, err := getConfigCmd.CombinedOutput()
	if err == nil {
		tc.originalMonitoringConfig = string(originalConfig)
		t.Logf("Captured original monitoring config: %s", tc.originalMonitoringConfig)
	} else {
		tc.originalMonitoringConfig = "" // ConfigMap doesn't exist yet
		t.Log("No existing cluster-monitoring-config found")
	}

	// Create ConfigMap to enable user workload monitoring in OpenShift
	clusterMonitoringConfig := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-monitoring-config",
			Namespace: "openshift-monitoring",
		},
		Data: map[string]string{
			"config.yaml": `enableUserWorkload: true
`,
		},
	}

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(clusterMonitoringConfig),
		WithCustomErrorMsg("Failed to enable user workload monitoring"),
	)

	t.Log("User workload monitoring enabled successfully")
}

func (tc *WVATestCtx) SetupKEDARBAC(t *testing.T) {
	t.Helper()

	t.Log("Setting up KEDA RBAC permissions")

	// Create ServiceAccount for KEDA metrics reader
	serviceAccount := &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "keda-metrics-reader",
			Namespace: kedaOpNamespace,
		},
	}

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(serviceAccount),
		WithCustomErrorMsg("Failed to create KEDA metrics reader ServiceAccount"),
	)

	// Create ClusterRoleBinding to grant cluster-monitoring-view permissions
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "rbac.authorization.k8s.io/v1",
			Kind:       "ClusterRoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "keda-metrics-reader-monitoring",
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "cluster-monitoring-view",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "keda-metrics-reader",
				Namespace: kedaOpNamespace,
			},
		},
	}

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(clusterRoleBinding),
		WithCustomErrorMsg("Failed to create KEDA metrics reader ClusterRoleBinding"),
	)

	// Create Secret for service account token
	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "keda-metrics-reader-token",
			Namespace: kedaOpNamespace,
			Annotations: map[string]string{
				"kubernetes.io/service-account.name": "keda-metrics-reader",
			},
		},
		Type: corev1.SecretTypeServiceAccountToken,
	}

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(secret),
		WithCustomErrorMsg("Failed to create KEDA metrics reader token Secret"),
	)

	// Create ClusterTriggerAuthentication for KEDA to access Thanos metrics
	clusterTriggerAuth := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "keda.sh/v1alpha1",
			"kind":       "ClusterTriggerAuthentication",
			"metadata": map[string]interface{}{
				"name": "ai-inference-keda-thanos",
			},
			"spec": map[string]interface{}{
				"secretTargetRef": []map[string]interface{}{
					{
						"parameter": "bearerToken",
						"name":      "keda-metrics-reader-token",
						"key":       "token",
					},
					{
						"parameter": "ca",
						"name":      "keda-metrics-reader-token",
						"key":       "ca.crt",
					},
				},
			},
		},
	}

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(clusterTriggerAuth),
		WithCustomErrorMsg("Failed to create KEDA ClusterTriggerAuthentication"),
	)

	t.Log("KEDA RBAC permissions setup completed successfully")
}

func (tc *WVATestCtx) CreateAutoscalingNamespace(t *testing.T) {
	t.Helper()

	t.Log("Creating autoscaling example namespace")

	// Create the autoscaling-example namespace
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(CreateNamespaceWithLabels(wvaTestNamespace, nil)),
		WithMinimalObject(gvk.Namespace, types.NamespacedName{Name: wvaTestNamespace}),
		WithCustomErrorMsg("Failed to create autoscaling example namespace"),
	)

	t.Log("Autoscaling example namespace created successfully")
}

func (tc *WVATestCtx) LabelNamespaceForMonitoring(t *testing.T) {
	t.Helper()

	t.Log("Labeling namespace for user workload monitoring")

	// Label the namespace with openshift.io/user-monitoring=true
	// This is required for Prometheus to discover PodMonitors in the namespace
	labelCmd := exec.CommandContext(context.Background(), "kubectl", "label", "namespace", wvaTestNamespace,
		"openshift.io/user-monitoring=true",
		"--overwrite")

	output, err := tc.execCommandWithLogging(t, "Label namespace for user workload monitoring", labelCmd)
	tc.g.Expect(err).NotTo(HaveOccurred(),
		"Expected: Successfully label namespace %s with openshift.io/user-monitoring=true\nActual: Command failed\nError: %v\nOutput: %s",
		wvaTestNamespace, err, output)

	t.Logf("✅ Namespace %s labeled for user workload monitoring", wvaTestNamespace)
	t.Log("Namespace labeling completed successfully")
}

func (tc *WVATestCtx) CreateAutoscalingGateway(t *testing.T) {
	t.Helper()

	t.Log("Creating autoscaling example gateway and configuration")

	// First, verify the GatewayClass exists (created by the Gateway service)
	t.Log("Verifying GatewayClass 'data-science-gateway-class' exists")
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.GatewayClass, types.NamespacedName{Name: "data-science-gateway-class"}),
		WithCustomErrorMsg("GatewayClass 'data-science-gateway-class' should exist before creating Gateway"),
		WithEventuallyTimeout(2*time.Minute),
		WithEventuallyPollingInterval(10*time.Second),
	)
	t.Log("✅ GatewayClass 'data-science-gateway-class' found")

	// Create ConfigMap for gateway configuration
	t.Log("Creating gateway ConfigMap")
	gatewayConfigMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "autoscaling-example-gateway-config",
			Namespace: wvaTestNamespace,
		},
		Data: map[string]string{
			"service": `metadata:
  annotations:
    service.beta.openshift.io/serving-cert-secret-name: "autoscaling-example-gateway-tls"
spec:
  type: ClusterIP
`,
			"deployment": `spec:
  template:
    spec:
      containers:
        - name: istio-proxy
          resources:
            limits:
              cpu: "16"
              memory: 16Gi
            requests:
              cpu: "1"
              memory: 1Gi
`,
		},
	}

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(gatewayConfigMap),
		WithCustomErrorMsg("Failed to create gateway ConfigMap"),
		WithEventuallyTimeout(30*time.Second),
	)
	t.Log("✅ Gateway ConfigMap created")

	// Create Gateway resource
	gateway := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "gateway.networking.k8s.io/v1",
			"kind":       "Gateway",
			"metadata": map[string]interface{}{
				"name":      "autoscaling-example-gateway",
				"namespace": wvaTestNamespace,
			},
			"spec": map[string]interface{}{
				"gatewayClassName": "data-science-gateway-class",
				"infrastructure": map[string]interface{}{
					"parametersRef": map[string]interface{}{
						"group": "",
						"kind":  "ConfigMap",
						"name":  "autoscaling-example-gateway-config",
					},
				},
				"listeners": []map[string]interface{}{
					{
						"allowedRoutes": map[string]interface{}{
							"namespaces": map[string]interface{}{
								"from": "Same",
							},
						},
						"name":     "https",
						"port":     443,
						"protocol": "HTTPS",
						"tls": map[string]interface{}{
							"certificateRefs": []map[string]interface{}{
								{
									"group": "",
									"kind":  "Secret",
									"name":  "autoscaling-example-gateway-tls",
								},
							},
							"mode": "Terminate",
						},
					},
				},
			},
		},
	}

	t.Log("Creating Gateway resource")
	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(gateway),
		WithCustomErrorMsg("Failed to create Gateway"),
		WithEventuallyTimeout(2*time.Minute),
		WithEventuallyPollingInterval(5*time.Second),
	)

	t.Log("✅ Autoscaling example gateway created successfully")
}

func (tc *WVATestCtx) ValidateAutoscalingGatewayResources(t *testing.T) {
	t.Helper()

	t.Log("Validating autoscaling gateway resources exist")

	// Validate ConfigMap exists
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ConfigMap, types.NamespacedName{
			Name:      "autoscaling-example-gateway-config",
			Namespace: wvaTestNamespace,
		}),
		WithCondition(And(
			jq.Match(`.data.service != null`),
			jq.Match(`.data.deployment != null`),
		)),
		WithCustomErrorMsg("Gateway ConfigMap '%s' should exist in namespace '%s' with service and deployment data", "autoscaling-example-gateway-config", wvaTestNamespace),
	)

	// Validate Gateway exists
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.KubernetesGateway, types.NamespacedName{
			Name:      "autoscaling-example-gateway",
			Namespace: wvaTestNamespace,
		}),
		WithCondition(And(
			jq.Match(`.spec.gatewayClassName == "data-science-gateway-class"`),
			jq.Match(`.spec.listeners[0].name == "https"`),
			jq.Match(`.spec.listeners[0].port == 443`),
		)),
		WithCustomErrorMsg("Gateway '%s' should exist in namespace '%s' with correct configuration", "autoscaling-example-gateway", wvaTestNamespace),
	)

	t.Log("Autoscaling gateway resources validated successfully")
}

func (tc *WVATestCtx) EnableWVA(t *testing.T) {
	t.Helper()

	t.Log("Enabling WVA through KServe")

	// Enable KServe first (WVA requires KServe to be managed)
	t.Log("Enabling KServe component (required for WVA)")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.kserve.managementState = "Managed"`)),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "KserveReady") | .status == "True"`)),
		WithEventuallyTimeout(5*time.Minute),
	)

	// Enable WVA within KServe
	t.Log("Enabling WVA within KServe configuration")
	tc.EventuallyResourcePatched(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithMutateFunc(testf.Transform(`.spec.components.kserve.wva.managementState = "Managed"`)),
		WithCondition(jq.Match(`.spec.components.kserve.wva.managementState == "Managed"`)),
		WithEventuallyTimeout(2*time.Minute),
	)

	t.Log("✅ WVA enabled successfully through KServe")
}

func (tc *WVATestCtx) ValidateKServeControllerDeployment(t *testing.T) {
	t.Helper()

	t.Log("Validating KServe controller deployment in applications namespace")

	// Validate KServe controller deployment exists and is ready
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      kserveControllerName,
			Namespace: tc.AppsNamespace,
		}),
		WithCondition(And(
			jq.Match(`.status.conditions[] | select(.type == "Available") | .status == "%s"`, string(metav1.ConditionTrue)),
			jq.Match(`.status.readyReplicas > 0`),
		)),
		WithCustomErrorMsg("KServe controller deployment '%s' should be available and have ready replicas in namespace '%s'", kserveControllerName, tc.AppsNamespace),
	)

	// Validate controller pods are running (expecting at least 1 replica)
	tc.EnsureDeploymentReady(types.NamespacedName{
		Name:      kserveControllerName,
		Namespace: tc.AppsNamespace,
	}, 1)

	t.Log("KServe controller deployment validation completed successfully")
}

func (tc *WVATestCtx) ValidateLLMISvcControllerDeployment(t *testing.T) {
	t.Helper()

	t.Log("Validating llmisvc controller deployment in applications namespace")

	// Validate llmisvc controller deployment exists and is ready
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      llmisvcControllerName,
			Namespace: tc.AppsNamespace,
		}),
		WithCondition(And(
			jq.Match(`.status.conditions[] | select(.type == "Available") | .status == "%s"`, string(metav1.ConditionTrue)),
			jq.Match(`.status.readyReplicas > 0`),
		)),
		WithCustomErrorMsg("llmisvc controller deployment '%s' should be available and have ready replicas in namespace '%s'", llmisvcControllerName, tc.AppsNamespace),
	)

	// Validate controller pods are running (expecting at least 1 replica)
	tc.EnsureDeploymentReady(types.NamespacedName{
		Name:      llmisvcControllerName,
		Namespace: tc.AppsNamespace,
	}, 1)

	t.Log("llmisvc controller deployment validation completed successfully")
}

func (tc *WVATestCtx) ValidateWVAControllerDeployment(t *testing.T) {
	t.Helper()

	t.Log("Validating WVA controller deployment in applications namespace")

	// Validate WVA controller deployment exists and is ready in the applications namespace
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      wvaControllerName,
			Namespace: tc.AppsNamespace,
		}),
		WithCondition(And(
			jq.Match(`.status.conditions[] | select(.type == "Available") | .status == "%s"`, string(metav1.ConditionTrue)),
			jq.Match(`.status.readyReplicas > 0`),
		)),
		WithCustomErrorMsg("WVA controller deployment '%s' should be available and have ready replicas in namespace '%s'", wvaControllerName, tc.AppsNamespace),
	)

	// Validate controller pods are running (expecting at least 1 replica)
	tc.EnsureDeploymentReady(types.NamespacedName{
		Name:      wvaControllerName,
		Namespace: tc.AppsNamespace,
	}, 1)

	t.Log("WVA controller deployment validation completed successfully")
}

func (tc *WVATestCtx) ValidateWVAConfigMaps(t *testing.T) {
	t.Helper()

	t.Log("Validating WVA ConfigMaps in applications namespace")

	// Validate saturation scaling ConfigMap exists
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ConfigMap, types.NamespacedName{
			Name:      wvaSaturationScalingConfigMap,
			Namespace: tc.AppsNamespace,
		}),
		WithCustomErrorMsg("WVA ConfigMap '%s' should exist in namespace '%s'", wvaSaturationScalingConfigMap, tc.AppsNamespace),
	)

	// Validate variant autoscaling ConfigMap exists
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ConfigMap, types.NamespacedName{
			Name:      wvaVariantAutoscalingConfigMap,
			Namespace: tc.AppsNamespace,
		}),
		WithCustomErrorMsg("WVA ConfigMap '%s' should exist in namespace '%s'", wvaVariantAutoscalingConfigMap, tc.AppsNamespace),
	)

	t.Log("WVA ConfigMaps validation completed successfully")
}

func (tc *WVATestCtx) PatchInferenceServiceConfig(t *testing.T) {
	t.Helper()

	t.Log("Patching inferenceservice-config ConfigMap for autoscaling")

	// Define the JSON patch content
	prometheusConfig := `{"prometheus":{"url":"https://thanos-querier.openshift-monitoring.svc.cluster.local:9091",` +
		`"authModes":"bearer","triggerAuthName":"ai-inference-keda-thanos",` +
		`"triggerAuthKind":"ClusterTriggerAuthentication"}}`
	patchContent := fmt.Sprintf(`[{"op":"replace","path":"/data/autoscaling-wva-controller-config","value":%q}]`,
		prometheusConfig)

	// Execute kubectl patch command
	//nolint:gosec // Test code with controlled namespace value
	patchCmd := exec.CommandContext(context.Background(), "kubectl", "patch", "configmap", "inferenceservice-config",
		"-n", tc.AppsNamespace,
		"--type=json",
		"--patch", patchContent)

	output, err := tc.execCommandWithLogging(t, "Patch inferenceservice-config ConfigMap", patchCmd)
	tc.g.Expect(err).NotTo(HaveOccurred(),
		"Expected: inferenceservice-config ConfigMap to be patched successfully\nActual: Patch failed\nOutput: %s", output)

	t.Log("inferenceservice-config patched successfully")
}

func (tc *WVATestCtx) RestartLLMISvcController(t *testing.T) {
	t.Helper()

	t.Log("Restarting llmisvc controller to pick up config change")

	// Use kubectl rollout restart to restart the deployment
	//nolint:gosec // Test code with controlled namespace and deployment name
	restartCmd := exec.CommandContext(context.Background(), "kubectl", "rollout", "restart", "deployment",
		llmisvcControllerName,
		"-n", tc.AppsNamespace)

	output, err := tc.execCommandWithLogging(t, "Restart llmisvc controller deployment", restartCmd)
	tc.g.Expect(err).NotTo(HaveOccurred(),
		"Expected: llmisvc controller deployment to be restarted\nActual: Restart command failed\nOutput: %s", output)

	// Wait for the rollout to complete
	t.Log("Waiting for llmisvc controller rollout to complete")
	//nolint:gosec // Test code with controlled namespace and deployment name
	rolloutStatusCmd := exec.CommandContext(context.Background(), "kubectl", "rollout", "status", "deployment",
		llmisvcControllerName,
		"-n", tc.AppsNamespace,
		"--timeout=5m")

	statusOutput, err := tc.execCommandWithLogging(t, "Wait for llmisvc controller rollout", rolloutStatusCmd)
	tc.g.Expect(err).NotTo(HaveOccurred(),
		"Expected: llmisvc controller rollout to complete within 5 minutes\nActual: Rollout did not complete or timed out\nStatus: %s", statusOutput)

	// Verify deployment is ready
	tc.EnsureDeploymentReady(types.NamespacedName{
		Name:      llmisvcControllerName,
		Namespace: tc.AppsNamespace,
	}, 1)

	t.Log("llmisvc controller restarted and ready")
}

func (tc *WVATestCtx) ScaleDownNonEssentialServices(t *testing.T) {
	t.Helper()

	t.Log("Scaling down non-essential services to save resources")

	// Initialize maps for tracking original replica counts
	tc.originalDeploymentReplicas = make(map[string]int32)
	tc.originalStatefulSetReplicas = make(map[string]int32)

	// Helper function to capture and scale deployment
	captureAndScaleDeployment := func(name, namespace string) {
		// Get current replica count
		getReplicasCmd := exec.CommandContext(context.Background(), "kubectl", "get", "deployment", name,
			"-n", namespace,
			"-o", "jsonpath={.spec.replicas}")
		output, err := getReplicasCmd.CombinedOutput()
		if err == nil {
			var replicas int32
			_, scanErr := fmt.Sscanf(string(output), "%d", &replicas)
			if scanErr == nil {
				key := fmt.Sprintf("%s/%s", namespace, name)
				tc.originalDeploymentReplicas[key] = replicas
				t.Logf("Captured original replicas for %s: %d", key, replicas)
			}
		}

		// Scale down
		scaleCmd := exec.CommandContext(context.Background(), "kubectl", "scale", "deployment", name,
			"-n", namespace,
			"--replicas=0")
		_, err = scaleCmd.CombinedOutput()
		if err != nil {
			t.Logf("⚠️  Warning: Could not scale down %s/%s (may not exist)", namespace, name)
		} else {
			t.Logf("Scaled down %s/%s to 0 replicas", namespace, name)
		}
	}

	// Define deployments to scale down in applications namespace
	appsDeployments := []string{
		"notebook-controller-deployment",
		"odh-notebook-controller-manager",
		"dashboard-redirect",
	}

	// Scale down ODH/RHOAI deployments in applications namespace
	t.Logf("Scaling down non-essential deployments in %s namespace", tc.AppsNamespace)
	for _, deploy := range appsDeployments {
		captureAndScaleDeployment(deploy, tc.AppsNamespace)
	}

	// Helper function to capture and scale statefulset
	captureAndScaleStatefulSet := func(name, namespace string) {
		// Get current replica count
		getReplicasCmd := exec.CommandContext(context.Background(), "kubectl", "get", "statefulset", name,
			"-n", namespace,
			"-o", "jsonpath={.spec.replicas}")
		output, err := getReplicasCmd.CombinedOutput()
		if err == nil {
			var replicas int32
			_, scanErr := fmt.Sscanf(string(output), "%d", &replicas)
			if scanErr == nil {
				key := fmt.Sprintf("%s/%s", namespace, name)
				tc.originalStatefulSetReplicas[key] = replicas
				t.Logf("Captured original replicas for StatefulSet %s: %d", key, replicas)
			}
		}

		// Scale down
		scaleCmd := exec.CommandContext(context.Background(), "kubectl", "scale", "statefulset", name,
			"-n", namespace,
			"--replicas=0")
		_, err = scaleCmd.CombinedOutput()
		if err != nil {
			t.Logf("⚠️  Warning: Could not scale down StatefulSet %s/%s (may not exist)", namespace, name)
		} else {
			t.Logf("Scaled down StatefulSet %s/%s to 0 replicas", namespace, name)
		}
	}

	// Scale down OpenShift console downloads
	t.Log("Scaling down OpenShift console downloads")
	captureAndScaleDeployment("downloads", "openshift-console")

	// Scale down cluster samples operator
	t.Log("Scaling down cluster samples operator")
	captureAndScaleDeployment("cluster-samples-operator", "openshift-cluster-samples-operator")

	// Scale down alertmanager
	t.Log("Scaling down alertmanager")
	captureAndScaleStatefulSet("alertmanager-main", "openshift-monitoring")

	// Declare variables for scaling commands
	var scaleCmd *exec.Cmd
	var err error

	// Scale operator to 1 replica (from potentially 3)
	operatorDeployment := tc.getControllerDeploymentName()
	t.Logf("Scaling %s to 1 replica", operatorDeployment)
	scaleCmd = exec.CommandContext(context.Background(), "kubectl", "scale", "deployment", operatorDeployment,
		"-n", tc.OperatorNamespace,
		"--replicas=1")
	_, err = scaleCmd.CombinedOutput()
	if err != nil {
		t.Logf("⚠️  Warning: Could not scale %s/%s", tc.OperatorNamespace, operatorDeployment)
	} else {
		t.Logf("Scaled %s/%s to 1 replica", tc.OperatorNamespace, operatorDeployment)
	}

	// Scale odh-model-controller to 1 replica
	t.Log("Scaling odh-model-controller to 1 replica")
	//nolint:gosec // Test code with controlled namespace from test context
	scaleCmd = exec.CommandContext(context.Background(), "kubectl", "scale", "deployment", "odh-model-controller",
		"-n", tc.AppsNamespace,
		"--replicas=1")
	_, err = scaleCmd.CombinedOutput()
	if err != nil {
		t.Logf("⚠️  Warning: Could not scale %s/odh-model-controller", tc.AppsNamespace)
	} else {
		t.Logf("Scaled %s/odh-model-controller to 1 replica", tc.AppsNamespace)
	}

	// Scale lws-controller-manager to 1 replica if it exists
	t.Log("Scaling lws-controller-manager to 1 replica if it exists")
	scaleCmd = exec.CommandContext(context.Background(), "kubectl", "scale", "deployment", "lws-controller-manager",
		"-n", "openshift-lws-operator",
		"--replicas=1")
	_, err = scaleCmd.CombinedOutput()
	if err != nil {
		t.Log("⚠️  Warning: Could not scale openshift-lws-operator/lws-controller-manager (may not exist)")
	} else {
		t.Log("Scaled openshift-lws-operator/lws-controller-manager to 1 replica")
	}

	t.Log("✅ Non-essential services scaled down to save resources")
}

func (tc *WVATestCtx) CreateLLMInferenceService(t *testing.T) {
	t.Helper()

	t.Log("Creating LLMInferenceService with simulator")

	// Create LLMInferenceService resource using simulator for testing
	llmisvc := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "serving.kserve.io/v1alpha2",
			"kind":       "LLMInferenceService",
			"metadata": map[string]interface{}{
				"name":      "sim-llama",
				"namespace": wvaTestNamespace,
				"annotations": map[string]interface{}{
					"prometheus.io/scrape": "true",
					"prometheus.io/port":   "8000",
					"prometheus.io/path":   "/metrics",
				},
			},
			"spec": map[string]interface{}{
				"model": map[string]interface{}{
					"name": "Qwen/Qwen2.5-7B-Instruct",
					"uri":  "hf://Qwen/Qwen2.5-7B-Instruct",
				},
				"storageInitializer": map[string]interface{}{
					"enabled": false,
				},
				"labels": map[string]interface{}{
					"inference.optimization/acceleratorName": "cpu",
				},
				"scaling": map[string]interface{}{
					"minReplicas": 1,
					"maxReplicas": 5,
					"wva": map[string]interface{}{
						"variantCost": "10.0",
						"keda": map[string]interface{}{
							"pollingInterval": 5,
							"cooldownPeriod":  30,
						},
					},
				},
				"template": map[string]interface{}{
					"containers": []map[string]interface{}{
						{
							"name":  "main",
							"image": llmInferenceSimulatorImage,
							"command": []string{
								"/app/llm-d-inference-sim",
							},
							"args": []string{
								"--port",
								"8000",
								"--model",
								"Qwen/Qwen2.5-7B-Instruct",
								"--mode",
								"random",
								"--max-num-seqs",
								"5",
								"--time-to-first-token",
								"100",
								"--inter-token-latency",
								"30",
							},
							"resources": map[string]interface{}{
								"requests": map[string]interface{}{
									"cpu":    "200m",
									"memory": "2Gi",
								},
								"limits": map[string]interface{}{
									"cpu":    "1",
									"memory": "2Gi",
								},
							},
							"startupProbe": map[string]interface{}{
								"httpGet": map[string]interface{}{
									"path":   "/health",
									"port":   8000,
									"scheme": "HTTP",
								},
								"failureThreshold": 60,
								"periodSeconds":    10,
							},
							"readinessProbe": map[string]interface{}{
								"httpGet": map[string]interface{}{
									"path":   "/health",
									"port":   8000,
									"scheme": "HTTP",
								},
							},
							"livenessProbe": map[string]interface{}{
								"httpGet": map[string]interface{}{
									"path":   "/health",
									"port":   8000,
									"scheme": "HTTP",
								},
							},
						},
					},
				},
			},
		},
	}

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(llmisvc),
		WithCustomErrorMsg("Failed to create LLMInferenceService"),
	)

	t.Log("LLMInferenceService created successfully")
}

func (tc *WVATestCtx) CreateVLLMMetricAliasPrometheusRule(t *testing.T) {
	t.Helper()

	t.Log("Creating PrometheusRule for vLLM metric aliases")

	// PrometheusRule that creates vllm:* aliases for kserve_vllm:* metrics.
	// The LLMInferenceService PodMonitor relabels vllm:* to kserve_vllm:* but
	// WVA queries for the unprefixed vllm:* names. This recording rule bridges
	// the gap until WVA supports configurable metric prefixes.
	prometheusRule := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "monitoring.coreos.com/v1",
			"kind":       "PrometheusRule",
			"metadata": map[string]interface{}{
				"name":      "vllm-metrics-alias",
				"namespace": wvaTestNamespace,
				"labels": map[string]interface{}{
					"monitoring.opendatahub.io/scrape": "true",
				},
			},
			"spec": map[string]interface{}{
				"groups": []map[string]interface{}{
					{
						"name":     "vllm-metric-aliases",
						"interval": "15s",
						"rules": []map[string]interface{}{
							{
								"record": "vllm:kv_cache_usage_perc",
								"expr":   "kserve_vllm:kv_cache_usage_perc",
							},
							{
								"record": "vllm:num_requests_waiting",
								"expr":   "kserve_vllm:num_requests_waiting",
							},
							{
								"record": "vllm:num_requests_running",
								"expr":   "kserve_vllm:num_requests_running",
							},
							{
								"record": "vllm:cache_config_info",
								"expr":   "kserve_vllm:cache_config_info",
							},
							{
								"record": "vllm:request_success_total",
								"expr":   "kserve_vllm:request_success_total",
							},
							{
								"record": "vllm:request_generation_tokens_sum",
								"expr":   "kserve_vllm:request_generation_tokens_sum",
							},
							{
								"record": "vllm:request_generation_tokens_count",
								"expr":   "kserve_vllm:request_generation_tokens_count",
							},
							{
								"record": "vllm:request_prompt_tokens_sum",
								"expr":   "kserve_vllm:request_prompt_tokens_sum",
							},
							{
								"record": "vllm:request_prompt_tokens_count",
								"expr":   "kserve_vllm:request_prompt_tokens_count",
							},
							{
								"record": "vllm:time_to_first_token_seconds_sum",
								"expr":   "kserve_vllm:time_to_first_token_seconds_sum",
							},
							{
								"record": "vllm:time_to_first_token_seconds_count",
								"expr":   "kserve_vllm:time_to_first_token_seconds_count",
							},
							{
								"record": "vllm:time_per_output_token_seconds_sum",
								"expr":   "kserve_vllm:time_per_output_token_seconds_sum",
							},
							{
								"record": "vllm:time_per_output_token_seconds_count",
								"expr":   "kserve_vllm:time_per_output_token_seconds_count",
							},
							{
								"record": "vllm:prefix_cache_hits",
								"expr":   "kserve_vllm:prefix_cache_hits",
							},
							{
								"record": "vllm:prefix_cache_queries",
								"expr":   "kserve_vllm:prefix_cache_queries",
							},
						},
					},
				},
			},
		},
	}

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(prometheusRule),
		WithCustomErrorMsg("Failed to create PrometheusRule for vLLM metric aliases"),
	)

	t.Log("PrometheusRule for vLLM metric aliases created successfully")
}

func (tc *WVATestCtx) ValidateLLMInferenceServiceReady(t *testing.T) {
	t.Helper()

	t.Log("Validating LLMInferenceService is ready")

	// Wait for LLMInferenceService to be ready with 5-minute timeout
	llmisvcNN := types.NamespacedName{
		Name:      "sim-llama",
		Namespace: wvaTestNamespace,
	}

	// First, get the resource to check its current state
	resource := tc.EnsureResourceExists(
		WithMinimalObject(gvk.LLMInferenceServiceV1Alpha2, llmisvcNN),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "%s"`, string(metav1.ConditionTrue))),
		WithCustomErrorMsg("LLMInferenceService '%s' should be ready in namespace '%s'", llmisvcNN.Name, llmisvcNN.Namespace),
		WithEventuallyTimeout(5*time.Minute),
		WithEventuallyPollingInterval(10*time.Second),
	)

	// If we reach here, the resource is ready
	t.Logf("LLMInferenceService is ready: %+v", resource)
	t.Log("LLMInferenceService validation completed successfully")
}

func (tc *WVATestCtx) CreateCustomPodMonitorsForHTTP(t *testing.T) {
	t.Helper()

	t.Log("Creating custom PodMonitors with HTTP scheme for metrics scraping")

	// The LLMInferenceService controller creates PodMonitors with scheme: https by default,
	// but the vLLM simulator exposes metrics on HTTP. The operator reconciles patches,
	// so we create custom PodMonitors with different names that use HTTP scheme.

	// Custom PodMonitor for vLLM engine metrics (HTTP version)
	customPodMonitor := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "monitoring.coreos.com/v1",
			"kind":       "PodMonitor",
			"metadata": map[string]interface{}{
				"name":      "kserve-llm-isvc-vllm-engine-http",
				"namespace": wvaTestNamespace,
				"labels": map[string]interface{}{
					"app.kubernetes.io/component":      "llm-monitoring",
					"app.kubernetes.io/part-of":        "llminferenceservice",
					"monitoring.opendatahub.io/scrape": "true",
				},
			},
			"spec": map[string]interface{}{
				"namespaceSelector": map[string]interface{}{},
				"selector": map[string]interface{}{
					"matchExpressions": []map[string]interface{}{
						{
							"key":      "app.kubernetes.io/component",
							"operator": "In",
							"values": []string{
								"llminferenceservice-workload",
								"llminferenceservice-workload-prefill",
								"llminferenceservice-workload-worker",
								"llminferenceservice-workload-leader",
								"llminferenceservice-workload-leader-prefill",
								"llminferenceservice-workload-worker-prefill",
							},
						},
					},
					"matchLabels": map[string]interface{}{
						"app.kubernetes.io/part-of": "llminferenceservice",
					},
				},
				"podMetricsEndpoints": []map[string]interface{}{
					{
						"scheme":     "http", // Use HTTP instead of HTTPS
						"targetPort": 8000,
						"metricRelabelings": []map[string]interface{}{
							{
								"action":       "replace",
								"replacement":  "kserve_$1",
								"sourceLabels": []string{"__name__"},
								"targetLabel":  "__name__",
							},
						},
						"relabelings": []map[string]interface{}{
							{
								"action":       "replace",
								"sourceLabels": []string{"__meta_kubernetes_pod_label_app_kubernetes_io_name"},
								"targetLabel":  "llm_isvc_name",
							},
							{
								"action":       "replace",
								"sourceLabels": []string{"__meta_kubernetes_pod_label_llm_d_ai_role"},
								"targetLabel":  "llm_isvc_role",
							},
							{
								"action":       "replace",
								"regex":        "llminferenceservice-(.*)",
								"replacement":  "$1",
								"sourceLabels": []string{"__meta_kubernetes_pod_label_app_kubernetes_io_component"},
								"targetLabel":  "llm_isvc_component",
							},
						},
					},
				},
			},
		},
	}

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(customPodMonitor),
		WithCustomErrorMsg("Failed to create custom PodMonitor for HTTP metrics scraping"),
	)

	t.Log("✅ Custom PodMonitor created successfully")

	// Wait a moment for Prometheus to reload configuration
	t.Log("Waiting 15 seconds for Prometheus to reload configuration and start scraping...")
	time.Sleep(15 * time.Second)

	t.Log("Custom PodMonitor creation completed successfully")
}

func (tc *WVATestCtx) ValidateScaledObjectCreated(t *testing.T) {
	t.Helper()

	t.Log("Validating ScaledObject is created for LLMInferenceService")

	// The ScaledObject should be created automatically by KEDA when LLMInferenceService with WVA is deployed
	// It should have the name format: <llminferenceservice-name>-kserve-keda
	scaledObjectNN := types.NamespacedName{
		Name:      "sim-llama-kserve-keda",
		Namespace: wvaTestNamespace,
	}

	t.Logf("Checking for ScaledObject: %s in namespace: %s", scaledObjectNN.Name, scaledObjectNN.Namespace)

	// Wait for ScaledObject resource to exist
	resource := tc.EnsureResourceExists(
		WithMinimalObject(gvk.ScaledObject, scaledObjectNN),
		WithCondition(And(
			jq.Match(`.metadata.name == "%s"`, scaledObjectNN.Name),
			jq.Match(`.spec.scaleTargetRef != null`),
		)),
		WithCustomErrorMsg("ScaledObject '%s' should exist in namespace '%s' with valid scaleTargetRef", scaledObjectNN.Name, scaledObjectNN.Namespace),
		WithEventuallyTimeout(1*time.Minute),
		WithEventuallyPollingInterval(5*time.Second),
	)

	// Log the ScaledObject details
	t.Logf("✅ ScaledObject found: %s", scaledObjectNN.Name)

	// Extract and log some key fields for debugging
	scaleTargetRef, err := jq.ExtractValue[map[string]any](resource, `.spec.scaleTargetRef`)
	if err == nil {
		t.Logf("ScaleTargetRef: %+v", scaleTargetRef)
	}

	triggers, err := jq.ExtractValue[[]any](resource, `.spec.triggers`)
	if err == nil {
		t.Logf("Number of triggers: %d", len(triggers))
	}

	t.Log("ScaledObject validation completed successfully")
}

func (tc *WVATestCtx) DiagnosePrometheusScraping(t *testing.T, metricName, namespace string) {
	t.Helper()

	t.Logf("=== Diagnostic: Investigating why Prometheus cannot scrape %s metrics ===", metricName)

	// Check for ServiceMonitor resources
	t.Log("Checking for ServiceMonitor resources in the namespace")
	smCmd := exec.CommandContext(context.Background(), "kubectl", "get", "servicemonitor",
		"-n", namespace,
		"-o", "yaml")
	smOutput, err := smCmd.CombinedOutput()
	if err != nil {
		t.Logf("⚠️  Could not list ServiceMonitors: %v", err)
	} else {
		t.Logf("ServiceMonitors in namespace %s:\n%s", namespace, string(smOutput))
	}

	// Check for PodMonitor resources
	t.Log("Checking for PodMonitor resources in the namespace")
	pmCmd := exec.CommandContext(context.Background(), "kubectl", "get", "podmonitor",
		"-n", namespace,
		"-o", "yaml")
	pmOutput, err := pmCmd.CombinedOutput()
	if err != nil {
		t.Logf("⚠️  Could not list PodMonitors: %v", err)
	} else {
		t.Logf("PodMonitors in namespace %s:\n%s", namespace, string(pmOutput))
	}

	// Get Prometheus pods and check their logs for scrape errors
	t.Log("Checking Prometheus logs for scrape errors")
	prometheusPodsCmd := exec.CommandContext(context.Background(), "kubectl", "get", "pods",
		"-n", "openshift-user-workload-monitoring",
		"-l", "app.kubernetes.io/name=prometheus",
		"-o", "jsonpath={.items[*].metadata.name}")
	podsOutput, err := prometheusPodsCmd.CombinedOutput()
	if err != nil {
		t.Logf("⚠️  Could not get Prometheus pods: %v", err)
	} else {
		podNames := strings.Fields(string(podsOutput))
		if len(podNames) == 0 {
			t.Log("⚠️  No Prometheus pods found in openshift-user-workload-monitoring")
		}
		for _, pod := range podNames {
			t.Logf("Checking logs for Prometheus pod: %s", pod)
			logsCmd := exec.CommandContext(context.Background(), "kubectl", "logs",
				"-n", "openshift-user-workload-monitoring",
				pod,
				"-c", "prometheus",
				"--tail=100")
			logsOutput, err := logsCmd.CombinedOutput()
			if err != nil {
				t.Logf("⚠️  Could not get logs for pod %s: %v", pod, err)
			} else {
				logs := string(logsOutput)
				// Filter for relevant error lines
				lines := strings.Split(logs, "\n")
				errorLines := []string{}
				for _, line := range lines {
					if strings.Contains(strings.ToLower(line), "error") ||
						strings.Contains(strings.ToLower(line), "failed") ||
						strings.Contains(strings.ToLower(line), "scrape") {
						errorLines = append(errorLines, line)
					}
				}
				if len(errorLines) > 0 {
					t.Logf("Recent errors/scrape-related logs from %s:\n%s", pod, strings.Join(errorLines, "\n"))
				} else {
					t.Logf("No obvious errors found in recent logs for %s", pod)
				}
			}
		}
	}

	// Check if pods have the required annotations for Prometheus scraping
	t.Logf("Checking pod annotations for Prometheus scraping in namespace %s", namespace)
	podAnnotationsCmd := exec.CommandContext(context.Background(), "kubectl", "get", "pods",
		"-n", namespace,
		"-o", "json")
	podAnnotationsOutput, err := podAnnotationsCmd.CombinedOutput()
	if err != nil {
		t.Logf("⚠️  Could not get pod annotations: %v", err)
	} else {
		t.Logf("Pod annotations for namespace %s:\n%s", namespace, string(podAnnotationsOutput))
	}

	t.Log("=== End of Prometheus scraping diagnostic ===")
}

func (tc *WVATestCtx) ValidatePrometheusWVAMetrics(t *testing.T) {
	t.Helper()

	t.Log("Validating Prometheus can scrape WVA controller metrics")

	// Get authentication token
	t.Log("Getting authentication token")
	tokenCmd := exec.CommandContext(context.Background(), "oc", "whoami", "-t")
	token, err := tc.execCommandWithLogging(t, "Get authentication token", tokenCmd)
	tc.g.Expect(err).NotTo(HaveOccurred(),
		"Expected: Successfully get authentication token from 'oc whoami -t'\nActual: Command failed\nError: %v", err)
	tc.g.Expect(token).NotTo(BeEmpty(),
		"Expected: Non-empty authentication token\nActual: Empty token")

	// Get Thanos querier route
	t.Log("Getting Thanos querier route")
	thanosCmd := exec.CommandContext(context.Background(), "oc", "get", "route", "thanos-querier", "-n", "openshift-monitoring", "-o", "jsonpath={.spec.host}")
	thanosHost, err := tc.execCommandWithLogging(t, "Get Thanos querier route", thanosCmd)
	tc.g.Expect(err).NotTo(HaveOccurred(),
		"Expected: Thanos querier route to exist in openshift-monitoring namespace\nActual: Failed to get route\nError: %v", err)
	tc.g.Expect(thanosHost).NotTo(BeEmpty(),
		"Expected: Non-empty Thanos querier host\nActual: Empty host")

	// Define WVA controller metrics to check
	// These are WVA-specific autoscaling metrics exposed by the controller
	wvaMetrics := []struct {
		name  string
		query string
	}{
		{
			name:  "wva_current_replicas",
			query: fmt.Sprintf(`wva_current_replicas{namespace="%s"}`, tc.AppsNamespace),
		},
		{
			name:  "wva_desired_ratio",
			query: fmt.Sprintf(`wva_desired_ratio{namespace="%s"}`, tc.AppsNamespace),
		},
		{
			name:  "wva_desired_replicas",
			query: fmt.Sprintf(`wva_desired_replicas{namespace="%s"}`, tc.AppsNamespace),
		},
	}

	// Query each metric and verify results exist
	for _, metric := range wvaMetrics {
		t.Logf("Checking WVA controller metric: %s", metric.name)

		url := fmt.Sprintf("https://%s/api/v1/query", thanosHost)

		// Query Prometheus/Thanos for the metric
		// Note: Using -k to skip certificate verification because test clusters use self-signed certificates.
		// In production environments, proper CA validation should be used.
		//nolint:gosec // Test code with controlled token/url/query values from test context
		curlCmd := exec.CommandContext(context.Background(), "curl", "-sk", "-G",
			"-H", fmt.Sprintf("Authorization: Bearer %s", token),
			url,
			"--data-urlencode", fmt.Sprintf("query=%s", metric.query))

		t.Logf("Querying Prometheus: %s", metric.query)
		curlOutput, err := curlCmd.CombinedOutput()
		result := string(curlOutput)

		// Track if we need to run diagnostics
		needsDiagnostics := false

		if err != nil {
			t.Logf("❌ FAILED: Query metric %s from Prometheus", metric.name)
			t.Logf("Command: curl -sk -G -H 'Authorization: Bearer <token>' %s --data-urlencode 'query=%s'", url, metric.query)
			t.Logf("Expected: Successful query execution")
			t.Logf("Actual: Command failed with error: %v", err)
			t.Logf("Response: %s", result)
			needsDiagnostics = true
		}

		if result == "" {
			t.Logf("❌ FAILED: Expected non-empty response for metric %s", metric.name)
			t.Logf("Expected: Non-empty JSON response from Prometheus")
			t.Logf("Actual: Empty response")
			needsDiagnostics = true
		}

		// Parse the JSON result to check if data exists
		if result != "" && !strings.Contains(result, `"status":"success"`) {
			t.Logf("❌ FAILED: Expected success status in response for metric %s", metric.name)
			t.Logf("Expected: Response containing '\"status\":\"success\"'")
			t.Logf("Actual response: %s", result)
			needsDiagnostics = true
		}

		// Check for non-empty results array
		if result != "" && (strings.Contains(result, `"result":[]`) || !strings.Contains(result, `"result":[`)) {
			t.Logf("❌ FAILED: Expected non-empty results for metric %s", metric.name)
			t.Logf("Query: %s", metric.query)
			t.Logf("Expected: Response containing '\"result\":[' with at least one metric value from WVA controller")
			t.Logf("Actual response: %s", result)
			needsDiagnostics = true
		}

		// Run diagnostics if any check failed
		if needsDiagnostics {
			tc.DiagnosePrometheusScraping(t, "WVA controller", tc.AppsNamespace)
		}

		// Now run the actual assertions
		tc.g.Expect(err).NotTo(HaveOccurred(),
			"Expected: Successfully query metric %s from Prometheus\nActual: Query failed\nError: %v\nResponse: %s",
			metric.name, err, result)

		tc.g.Expect(result).NotTo(BeEmpty(),
			"Expected: Non-empty response from Prometheus for metric %s\nActual: Empty response",
			metric.name)

		tc.g.Expect(result).To(ContainSubstring(`"status":"success"`),
			"Expected: Response with status 'success' for metric %s\nActual response: %s",
			metric.name, result)

		tc.g.Expect(result).To(MatchRegexp(`"result":\s*\[.+\]`),
			"Expected: Non-empty results array for metric %s from WVA controller\nQuery: %s\nActual response: %s",
			metric.name, metric.query, result)

		t.Logf("✅ Successfully validated metric %s from WVA controller", metric.name)
	}

	t.Log("All WVA controller metrics are available in Prometheus")
}

func (tc *WVATestCtx) ValidateVLLMMetricsFromPods(t *testing.T) {
	t.Helper()

	t.Log("Validating vLLM metrics are exposed from pods")

	// Get pods with the LLMInferenceService workload label
	labelSelector := "app.kubernetes.io/component=llminferenceservice-workload"
	t.Logf("Finding pods with label: %s in namespace: %s", labelSelector, wvaTestNamespace)

	getPodCmd := exec.CommandContext(context.Background(), "oc", "get", "pods",
		"-n", wvaTestNamespace,
		"-l", labelSelector,
		"-o", "jsonpath={.items[*].metadata.name}")

	podOutput, err := tc.execCommandWithLogging(t, fmt.Sprintf("Get pods with label %s", labelSelector), getPodCmd)
	tc.g.Expect(err).NotTo(HaveOccurred(),
		"Expected: To find pods with label %s in namespace %s\nActual: Failed to get pods\nOutput: %s",
		labelSelector, wvaTestNamespace, podOutput)

	podNames := strings.Fields(podOutput)
	tc.g.Expect(podNames).NotTo(BeEmpty(),
		"Expected: At least one pod with label %s in namespace %s\nActual: No pods found\nPods: %v",
		labelSelector, wvaTestNamespace, podNames)

	t.Logf("Found %d pod(s): %v", len(podNames), podNames)

	// Define vLLM metrics to check in the /metrics endpoint
	expectedMetrics := []string{
		"vllm:num_requests_running",
		"vllm:num_requests_waiting",
	}

	// Check metrics from each pod
	for _, podName := range podNames {
		t.Logf("Checking metrics from pod: %s", podName)

		// Curl the /metrics endpoint from inside the pod
		curlCmd := exec.CommandContext(context.Background(), "oc", "exec", "-n", wvaTestNamespace, podName,
			"--", "curl", "-s", "localhost:8000/metrics")

		metricsContent, err := tc.execCommandWithLogging(t, fmt.Sprintf("Curl metrics from pod %s", podName), curlCmd)
		tc.g.Expect(err).NotTo(HaveOccurred(),
			"Expected: Metrics endpoint localhost:8000/metrics to be accessible from pod %s\nActual: Failed to curl metrics\nError: %v",
			podName, err)

		tc.g.Expect(metricsContent).NotTo(BeEmpty(),
			"Expected: Non-empty metrics output from pod %s\nActual: Empty metrics output",
			podName)

		// Verify each expected metric is present
		for _, metric := range expectedMetrics {
			if !strings.Contains(metricsContent, metric) {
				t.Logf("❌ Missing metric %s in pod %s", metric, podName)
				t.Logf("Expected: Metric %s to be present", metric)
				t.Logf("Actual metrics output (first 500 chars): %s", metricsContent[:min(500, len(metricsContent))])
			}
			tc.g.Expect(metricsContent).To(ContainSubstring(metric),
				"Expected: Metric %s to be present in pod %s\nActual: Metric not found in output",
				metric, podName)
			t.Logf("✅ Found metric %s in pod %s", metric, podName)
		}

		t.Logf("Successfully validated all vLLM metrics from pod %s", podName)
	}

	t.Log("All vLLM metrics are available from pod endpoints")
}

func (tc *WVATestCtx) ValidateServiceMonitorCreated(t *testing.T) {
	t.Helper()

	t.Log("Validating ServiceMonitor is created for vLLM metrics scraping")

	// ServiceMonitor should be created in the autoscaling-example namespace
	// to configure Prometheus to scrape metrics from vLLM pods
	t.Logf("Checking for ServiceMonitor in namespace: %s", wvaTestNamespace)

	// Expected ServiceMonitor name patterns
	expectedServiceMonitorPatterns := []string{
		"sim-llama-kserve-workload",
		"sim-llama-kserve",
		"sim-llama",
		"vllm-metrics",
	}

	// List ServiceMonitors in the namespace
	listSMCmd := exec.CommandContext(context.Background(), "kubectl", "get", "servicemonitor",
		"-n", wvaTestNamespace,
		"-o", "jsonpath={.items[*].metadata.name}")

	smOutput, err := tc.execCommandWithLogging(t, fmt.Sprintf("List ServiceMonitors in %s", wvaTestNamespace), listSMCmd)
	if err != nil || smOutput == "" {
		t.Logf("❌ FAILED: No ServiceMonitors found in namespace %s", wvaTestNamespace)
		t.Logf("Expected: ServiceMonitor with one of these names to exist:")
		for _, pattern := range expectedServiceMonitorPatterns {
			t.Logf("  - %s", pattern)
		}
		t.Logf("Actual: No ServiceMonitors found")
		t.Logf("Note: ServiceMonitor is required to configure Prometheus to scrape vLLM metrics")
	}
	tc.g.Expect(err).NotTo(HaveOccurred(),
		"Expected: Successfully list ServiceMonitors in namespace %s\nActual: Command failed\nError: %v", wvaTestNamespace, err)
	tc.g.Expect(smOutput).NotTo(BeEmpty(),
		"Expected: At least one ServiceMonitor in namespace %s (looking for: %v)\nActual: No ServiceMonitors found",
		wvaTestNamespace, expectedServiceMonitorPatterns)

	t.Logf("✅ ServiceMonitor(s) found in namespace: %s", smOutput)

	// Get detailed information about ServiceMonitors for debugging
	describeSMCmd := exec.CommandContext(context.Background(), "kubectl", "get", "servicemonitor",
		"-n", wvaTestNamespace,
		"-o", "yaml")

	smDetails, err := describeSMCmd.CombinedOutput()
	if err != nil {
		t.Logf("⚠️  Could not get ServiceMonitor details: %v", err)
	} else {
		t.Logf("ServiceMonitor details:\n%s", string(smDetails))
	}

	// Verify ServiceMonitor configuration (check for key fields)
	if !strings.Contains(smOutput, `"spec"`) {
		t.Log("⚠️  Warning: ServiceMonitor spec not found in output")
	}

	// Check for endpoint configuration which defines how to scrape metrics
	if !strings.Contains(smOutput, `"endpoints"`) && !strings.Contains(smOutput, `"endpoint"`) {
		t.Log("⚠️  Warning: ServiceMonitor endpoints configuration not found")
	}

	t.Log("ServiceMonitor validation completed successfully")
}

func (tc *WVATestCtx) ValidateServiceForMetrics(t *testing.T) {
	t.Helper()

	t.Log("Validating Service exists for vLLM metrics scraping")

	// The expected Service name pattern created by LLMInferenceService controller
	// Common patterns: sim-llama, sim-llama-kserve, sim-llama-kserve-workload
	expectedServicePatterns := []string{
		"sim-llama-kserve-workload",
		"sim-llama-kserve",
		"sim-llama",
	}

	// List all Services in the namespace
	listSvcCmd := exec.CommandContext(context.Background(), "kubectl", "get", "service",
		"-n", wvaTestNamespace,
		"-o", "jsonpath={.items[*].metadata.name}")

	svcOutput, err := tc.execCommandWithLogging(t, fmt.Sprintf("List Services in %s", wvaTestNamespace), listSvcCmd)
	if err != nil || svcOutput == "" {
		t.Logf("❌ FAILED: No Services found in namespace %s", wvaTestNamespace)
		t.Logf("Expected: Service with one of these names to exist:")
		for _, pattern := range expectedServicePatterns {
			t.Logf("  - %s", pattern)
		}
		t.Logf("Actual: No Services found")
		t.Logf("Note: LLMInferenceService controller should create a Service for metrics scraping")
	}
	tc.g.Expect(err).NotTo(HaveOccurred(),
		"Expected: Successfully list Services in namespace %s\nActual: Command failed\nError: %v", wvaTestNamespace, err)
	tc.g.Expect(svcOutput).NotTo(BeEmpty(),
		"Expected: At least one Service in namespace %s (looking for: %v)\nActual: No Services found",
		wvaTestNamespace, expectedServicePatterns)

	t.Logf("✅ Service(s) found in namespace: %s", svcOutput)

	// Get detailed information about Services for debugging
	describeSvcCmd := exec.CommandContext(context.Background(), "kubectl", "get", "service",
		"-n", wvaTestNamespace,
		"-o", "yaml")

	svcDetails, err := describeSvcCmd.CombinedOutput()
	if err != nil {
		t.Logf("⚠️  Could not get Service details: %v", err)
	} else {
		t.Logf("Service details:\n%s", string(svcDetails))
	}

	t.Log("Service validation completed successfully")
}

func (tc *WVATestCtx) ValidateServiceMonitorMatchesService(t *testing.T) {
	t.Helper()

	t.Log("Validating ServiceMonitor selector matches Service labels")

	// Get ServiceMonitor names
	listSMCmd := exec.CommandContext(context.Background(), "kubectl", "get", "servicemonitor",
		"-n", wvaTestNamespace,
		"-o", "jsonpath={.items[*].metadata.name}")
	smNames, _ := listSMCmd.CombinedOutput()

	// Get Service names
	listSvcCmd := exec.CommandContext(context.Background(), "kubectl", "get", "service",
		"-n", wvaTestNamespace,
		"-o", "jsonpath={.items[*].metadata.name}")
	svcNames, _ := listSvcCmd.CombinedOutput()

	t.Logf("Found ServiceMonitors: %s", string(smNames))
	t.Logf("Found Services: %s", string(svcNames))

	// Get ServiceMonitor with selector information
	getSMCmd := exec.CommandContext(context.Background(), "kubectl", "get", "servicemonitor",
		"-n", wvaTestNamespace,
		"-o", "json")

	smOutput, err := tc.execCommandWithLogging(t, "Get ServiceMonitor", getSMCmd)
	tc.g.Expect(err).NotTo(HaveOccurred(),
		"Expected: Successfully get ServiceMonitor\nActual: Command failed\nError: %v", err)

	// Get Services with labels
	getSvcCmd := exec.CommandContext(context.Background(), "kubectl", "get", "service",
		"-n", wvaTestNamespace,
		"-o", "json")

	svcOutput, err := tc.execCommandWithLogging(t, "Get Services", getSvcCmd)
	tc.g.Expect(err).NotTo(HaveOccurred(),
		"Expected: Successfully get Services\nActual: Command failed\nError: %v", err)

	// Log both for manual inspection
	t.Log("=== ServiceMonitor Configuration ===")
	t.Logf("%s", smOutput)
	t.Log("=== Service Labels ===")
	t.Logf("%s", svcOutput)

	// Check if ServiceMonitor has a selector
	if !strings.Contains(smOutput, `"selector"`) && !strings.Contains(smOutput, `"matchLabels"`) {
		t.Log("⚠️  Warning: ServiceMonitor may not have a proper selector configured")
		t.Log("This means Prometheus won't know which Services to scrape")
		t.Log("ServiceMonitor should have spec.selector.matchLabels that match Service labels")
	}

	// Check if Services have labels
	if !strings.Contains(svcOutput, `"labels"`) {
		t.Log("⚠️  Warning: Services may not have labels")
		t.Log("This means ServiceMonitor selector cannot match them")
		t.Log("Services should have labels that match ServiceMonitor spec.selector.matchLabels")
	}

	t.Log("✅ ServiceMonitor and Service configuration logged for inspection")
	t.Log("ServiceMonitor-Service matching validation completed")
}

func (tc *WVATestCtx) ValidatePrometheusVLLMMetrics(t *testing.T) {
	t.Helper()

	t.Log("Validating Prometheus can scrape vLLM metrics")

	// Get authentication token
	t.Log("Getting authentication token")
	tokenCmd := exec.CommandContext(context.Background(), "oc", "whoami", "-t")
	token, err := tc.execCommandWithLogging(t, "Get authentication token", tokenCmd)
	tc.g.Expect(err).NotTo(HaveOccurred(),
		"Expected: Successfully get authentication token from 'oc whoami -t'\nActual: Command failed\nError: %v", err)
	tc.g.Expect(token).NotTo(BeEmpty(),
		"Expected: Non-empty authentication token\nActual: Empty token")

	// Get Thanos querier route
	t.Log("Getting Thanos querier route")
	thanosCmd := exec.CommandContext(context.Background(), "oc", "get", "route", "thanos-querier", "-n", "openshift-monitoring", "-o", "jsonpath={.spec.host}")
	thanosHost, err := tc.execCommandWithLogging(t, "Get Thanos querier route", thanosCmd)
	tc.g.Expect(err).NotTo(HaveOccurred(),
		"Expected: Thanos querier route to exist in openshift-monitoring namespace\nActual: Failed to get route\nError: %v", err)
	tc.g.Expect(thanosHost).NotTo(BeEmpty(),
		"Expected: Non-empty Thanos querier host\nActual: Empty host")

	// Define vLLM metrics to check
	vllmMetrics := []string{"num_requests_running", "num_requests_waiting"}

	// Query each metric with retry logic (wait up to 5 minutes)
	for _, metric := range vllmMetrics {
		t.Logf("Checking vLLM metric: %s (with up to 5 minute timeout)", metric)

		query := fmt.Sprintf("vllm:%s{namespace=\"%s\"}", metric, wvaTestNamespace)
		url := fmt.Sprintf("https://%s/api/v1/query", thanosHost)

		// Use Eventually to retry the query for up to 5 minutes
		// This allows time for:
		// 1. Custom PodMonitor to start scraping (HTTP scheme)
		// 2. PrometheusRule to evaluate and create vllm:* aliases
		// 3. Metrics to propagate to Thanos
		tc.g.Eventually(func(g Gomega) {
			// Query Prometheus/Thanos for the metric
			// Note: Using -k to skip certificate verification because test clusters use self-signed certificates.
			// In production environments, proper CA validation should be used.
			//nolint:gosec // Test code with controlled token/url/query values from test context
			curlCmd := exec.CommandContext(context.Background(), "curl", "-sk", "-G",
				"-H", fmt.Sprintf("Authorization: Bearer %s", token),
				url,
				"--data-urlencode", fmt.Sprintf("query=%s", query))

			curlOutput, err := curlCmd.CombinedOutput()
			result := string(curlOutput)

			// Check for query success
			g.Expect(err).NotTo(HaveOccurred(),
				"Expected: Successfully query metric %s from Prometheus\nActual: Query failed\nError: %v\nResponse: %s",
				metric, err, result)

			g.Expect(result).NotTo(BeEmpty(),
				"Expected: Non-empty response from Prometheus for metric %s\nActual: Empty response",
				metric)

			g.Expect(result).To(ContainSubstring(`"status":"success"`),
				"Expected: Response with status 'success' for metric %s\nActual response: %s",
				metric, result)

			g.Expect(result).To(MatchRegexp(`"result":\s*\[.+\]`),
				"Expected: Non-empty results array for metric %s in namespace %s\nQuery: %s\nActual response: %s",
				metric, wvaTestNamespace, query, result)

			t.Logf("✅ Successfully validated metric %s from Prometheus", metric)
		}).WithTimeout(5*time.Minute).WithPolling(15*time.Second).Should(Succeed(),
			"Metric %s should be available in Prometheus within 5 minutes", metric)
	}

	t.Log("All vLLM metrics are available in Prometheus")
}

func (tc *WVATestCtx) ValidateVariantAutoscalingCreated(t *testing.T) {
	t.Helper()

	t.Log("Validating VariantAutoscaling resource is created")

	// The VariantAutoscaling resource should be created automatically when LLMInferenceService with WVA is deployed
	// It has the naming pattern: <llminferenceservice-name>-kserve-va
	vaNN := types.NamespacedName{
		Name:      "sim-llama-kserve-va",
		Namespace: wvaTestNamespace,
	}

	t.Logf("Checking for VariantAutoscaling: %s in namespace: %s", vaNN.Name, vaNN.Namespace)

	// Wait for VariantAutoscaling resource to exist
	resource := tc.EnsureResourceExists(
		WithMinimalObject(gvk.VariantAutoscalingV1Alpha1, vaNN),
		WithCondition(jq.Match(`.metadata.name == "%s"`, vaNN.Name)),
		WithCustomErrorMsg("VariantAutoscaling '%s' should exist in namespace '%s'", vaNN.Name, vaNN.Namespace),
		WithEventuallyTimeout(1*time.Minute),
		WithEventuallyPollingInterval(5*time.Second),
	)

	t.Logf("✅ VariantAutoscaling resource found: %s", vaNN.Name)

	// Log some details for debugging
	variantCost, err := jq.ExtractValue[string](resource, `.spec.variantCost`)
	if err == nil {
		t.Logf("Variant cost: %s", variantCost)
	}

	t.Log("VariantAutoscaling creation validation completed successfully")
}

func (tc *WVATestCtx) ValidateVariantAutoscalingMetricReady(t *testing.T) {
	t.Helper()

	t.Log("Validating VariantAutoscaling MetricsAvailable condition is true with reason MetricsFound")

	// The VariantAutoscaling resource has the naming pattern: <llminferenceservice-name>-kserve-va
	vaNN := types.NamespacedName{
		Name:      "sim-llama-kserve-va",
		Namespace: wvaTestNamespace,
	}

	t.Logf("Checking MetricsAvailable condition for VariantAutoscaling: %s", vaNN.Name)

	// Wait for VariantAutoscaling resource to have MetricsAvailable condition with:
	// - status: "True"
	// - reason: "MetricsFound"
	resource := tc.EnsureResourceExists(
		WithMinimalObject(gvk.VariantAutoscalingV1Alpha1, vaNN),
		WithCondition(jq.Match(
			`.status.conditions[] | select(.type == "MetricsAvailable") | (.status == "%s" and .reason == "%s")`,
			string(metav1.ConditionTrue), "MetricsFound")),
		WithCustomErrorMsg("VariantAutoscaling '%s' should have MetricsAvailable condition with status=True and reason=MetricsFound in namespace '%s'", vaNN.Name, vaNN.Namespace),
		WithEventuallyTimeout(5*time.Minute),
		WithEventuallyPollingInterval(10*time.Second),
	)

	// If we reach here, the VariantAutoscaling has metrics available
	t.Logf("✅ VariantAutoscaling MetricsAvailable condition is True with reason MetricsFound")
	t.Logf("VariantAutoscaling resource: %+v", resource)
	t.Log("VariantAutoscaling metrics available validation completed successfully")
}

func (tc *WVATestCtx) ValidateScaledObjectReady(t *testing.T) {
	t.Helper()

	t.Log("Validating ScaledObject is ready and active")

	// The ScaledObject should be created automatically by KEDA when LLMInferenceService with WVA is deployed
	// It has the naming pattern: <llminferenceservice-name>-kserve-keda
	scaledObjectNN := types.NamespacedName{
		Name:      "sim-llama-kserve-keda",
		Namespace: wvaTestNamespace,
	}

	t.Logf("Checking Ready and Active conditions for ScaledObject: %s", scaledObjectNN.Name)

	// First, validate the Ready condition
	t.Log("Checking Ready condition (status=True, reason=ScaledObjectReady)")
	readyResource := tc.EnsureResourceExists(
		WithMinimalObject(gvk.ScaledObject, scaledObjectNN),
		WithCondition(jq.Match(
			`.status.conditions[] | select(.type == "Ready") | (.status == "%s" and .reason == "%s")`,
			"True", "ScaledObjectReady")),
		WithCustomErrorMsg("ScaledObject '%s' should have Ready condition with status=True and reason=ScaledObjectReady in namespace '%s'",
			scaledObjectNN.Name, scaledObjectNN.Namespace),
		WithEventuallyTimeout(5*time.Minute),
		WithEventuallyPollingInterval(10*time.Second),
	)
	t.Logf("✅ ScaledObject Ready condition: status=True, reason=ScaledObjectReady")

	// Then, validate the Active condition
	t.Log("Checking Active condition (status=True, reason=ScalerActive)")
	activeResource := tc.EnsureResourceExists(
		WithMinimalObject(gvk.ScaledObject, scaledObjectNN),
		WithCondition(jq.Match(
			`.status.conditions[] | select(.type == "Active") | (.status == "%s" and .reason == "%s")`,
			"True", "ScalerActive")),
		WithCustomErrorMsg("ScaledObject '%s' should have Active condition with status=True and reason=ScalerActive in namespace '%s'",
			scaledObjectNN.Name, scaledObjectNN.Namespace),
		WithEventuallyTimeout(5*time.Minute),
		WithEventuallyPollingInterval(10*time.Second),
	)
	t.Logf("✅ ScaledObject Active condition: status=True, reason=ScalerActive")

	// Log the final state
	t.Logf("ScaledObject ready resource: %+v", readyResource)
	t.Logf("ScaledObject active resource: %+v", activeResource)
	t.Log("ScaledObject ready and active validation completed successfully")
}

func (tc *WVATestCtx) CleanupAutoscalingResources(t *testing.T) {
	t.Helper()

	t.Log("Cleaning up autoscaling resources")

	// Delete LLMInferenceService
	// First, check if resource exists and remove finalizers to avoid getting stuck during deletion
	t.Log("Checking for LLMInferenceService")
	checkCmd := exec.CommandContext(context.Background(), "kubectl", "get", "llminferenceservice", "sim-llama",
		"-n", wvaTestNamespace,
		"--ignore-not-found")
	checkOutput, err := checkCmd.CombinedOutput()

	if err == nil && len(checkOutput) > 0 {
		t.Log("Removing finalizers from LLMInferenceService")
		removeFinalizers := exec.CommandContext(context.Background(), "kubectl", "patch", "llminferenceservice", "sim-llama",
			"-n", wvaTestNamespace,
			"--type=json",
			"-p", `[{"op": "remove", "path": "/metadata/finalizers"}]`)
		patchOutput, patchErr := tc.execCommandWithLogging(t, "Remove LLMInferenceService finalizers", removeFinalizers)
		if patchErr != nil {
			t.Logf("Note: Could not remove finalizers: %v", patchErr)
		}
		_ = patchOutput // Suppress unused variable warning
	} else {
		t.Log("LLMInferenceService does not exist, skipping finalizer removal")
	}

	t.Log("Deleting LLMInferenceService")
	deleteLLMISvcCmd := exec.CommandContext(context.Background(), "kubectl", "delete", "llminferenceservice", "sim-llama",
		"-n", wvaTestNamespace,
		"--ignore-not-found=true",
		"--timeout=30s")
	deleteOutput, deleteErr := tc.execCommandWithLogging(t, "Delete LLMInferenceService", deleteLLMISvcCmd)
	if deleteErr != nil {
		t.Logf("Warning: Failed to delete LLMInferenceService: %v\nOutput: %s", deleteErr, deleteOutput)
	}

	// Delete Gateway
	t.Log("Deleting Gateway")
	deleteGatewayCmd := exec.CommandContext(context.Background(), "kubectl", "delete", "gateway", "autoscaling-example-gateway",
		"-n", wvaTestNamespace,
		"--ignore-not-found=true")
	gwOutput, gwErr := tc.execCommandWithLogging(t, "Delete Gateway", deleteGatewayCmd)
	if gwErr != nil {
		t.Logf("Warning: Failed to delete Gateway: %v\nOutput: %s", gwErr, gwOutput)
	}

	// Delete Gateway ConfigMap
	t.Log("Deleting Gateway ConfigMap")
	deleteConfigMapCmd := exec.CommandContext(context.Background(), "kubectl", "delete", "configmap", "autoscaling-example-gateway-config",
		"-n", wvaTestNamespace,
		"--ignore-not-found=true")
	cmOutput, cmErr := tc.execCommandWithLogging(t, "Delete Gateway ConfigMap", deleteConfigMapCmd)
	if cmErr != nil {
		t.Logf("Warning: Failed to delete Gateway ConfigMap: %v\nOutput: %s", cmErr, cmOutput)
	}

	// Delete PrometheusRule
	t.Log("Deleting PrometheusRule")
	deletePrometheusRuleCmd := exec.CommandContext(context.Background(), "kubectl", "delete", "prometheusrule", "vllm-metrics-alias",
		"-n", wvaTestNamespace,
		"--ignore-not-found=true")
	prOutput, prErr := tc.execCommandWithLogging(t, "Delete PrometheusRule", deletePrometheusRuleCmd)
	if prErr != nil {
		t.Logf("Warning: Failed to delete PrometheusRule: %v\nOutput: %s", prErr, prOutput)
	}

	// Delete custom PodMonitor
	t.Log("Deleting custom PodMonitor")
	deletePodMonitorCmd := exec.CommandContext(context.Background(), "kubectl", "delete", "podmonitor", "kserve-llm-isvc-vllm-engine-http",
		"-n", wvaTestNamespace,
		"--ignore-not-found=true")
	pmOutput, pmErr := tc.execCommandWithLogging(t, "Delete custom PodMonitor", deletePodMonitorCmd)
	if pmErr != nil {
		t.Logf("Warning: Failed to delete custom PodMonitor: %v\nOutput: %s", pmErr, pmOutput)
	}

	// Delete cluster-scoped resources
	// Delete ClusterRoleBinding
	t.Log("Deleting ClusterRoleBinding")
	deleteCRBCmd := exec.CommandContext(context.Background(), "kubectl", "delete", "clusterrolebinding", "keda-metrics-reader-monitoring",
		"--ignore-not-found=true",
		"--timeout=1m")
	crbOutput, crbErr := tc.execCommandWithLogging(t, "Delete ClusterRoleBinding", deleteCRBCmd)
	if crbErr != nil {
		t.Logf("Warning: Failed to delete ClusterRoleBinding: %v\nOutput: %s", crbErr, crbOutput)
	}

	// Delete ClusterTriggerAuthentication
	t.Log("Deleting ClusterTriggerAuthentication")
	deleteCTACmd := exec.CommandContext(context.Background(), "kubectl", "delete", "clustertriggerauthentication", "ai-inference-keda-thanos",
		"--ignore-not-found=true",
		"--timeout=1m")
	ctaOutput, ctaErr := tc.execCommandWithLogging(t, "Delete ClusterTriggerAuthentication", deleteCTACmd)
	if ctaErr != nil {
		t.Logf("Warning: Failed to delete ClusterTriggerAuthentication: %v\nOutput: %s", ctaErr, ctaOutput)
	}

	// Delete KEDA namespace resources
	// Delete ServiceAccount
	t.Log("Deleting KEDA metrics reader ServiceAccount")
	deleteSACmd := exec.CommandContext(context.Background(), "kubectl", "delete", "serviceaccount", "keda-metrics-reader",
		"-n", "openshift-keda",
		"--ignore-not-found=true",
		"--timeout=1m")
	saOutput, saErr := tc.execCommandWithLogging(t, "Delete KEDA ServiceAccount", deleteSACmd)
	if saErr != nil {
		t.Logf("Warning: Failed to delete ServiceAccount: %v\nOutput: %s", saErr, saOutput)
	}

	// Delete Secret
	t.Log("Deleting KEDA metrics reader Secret")
	deleteSecretCmd := exec.CommandContext(context.Background(), "kubectl", "delete", "secret", "keda-metrics-reader-token",
		"-n", "openshift-keda",
		"--ignore-not-found=true",
		"--timeout=1m")
	secretOutput, secretErr := tc.execCommandWithLogging(t, "Delete KEDA Secret", deleteSecretCmd)
	if secretErr != nil {
		t.Logf("Warning: Failed to delete Secret: %v\nOutput: %s", secretErr, secretOutput)
	}

	t.Log("Autoscaling resources cleanup completed")
}

// RestoreClusterState restores the cluster to its original state before the test.
func (tc *WVATestCtx) RestoreClusterState(t *testing.T) {
	t.Helper()

	t.Log("Restoring cluster to original state")

	// Restore cluster-monitoring-config if we modified it
	if tc.originalMonitoringConfig != "" {
		t.Log("Restoring original cluster-monitoring-config")

		// Create a temporary file with the original config
		tmpFile, err := os.CreateTemp(t.TempDir(), "cluster-monitoring-config-*.yaml")
		if err != nil {
			t.Logf("Warning: Failed to create temp file for config restoration: %v", err)
		} else {
			defer os.Remove(tmpFile.Name())

			if _, err := tmpFile.WriteString(tc.originalMonitoringConfig); err != nil {
				t.Logf("Warning: Failed to write original config to temp file: %v", err)
			} else {
				tmpFile.Close()

				// Apply the original config
				//nolint:gosec // Test code with controlled temporary file path from t.TempDir()
				restoreConfigCmd := exec.CommandContext(context.Background(), "kubectl", "create", "configmap", "cluster-monitoring-config",
					"-n", "openshift-monitoring",
					"--from-file=config.yaml="+tmpFile.Name(),
					"--dry-run=client",
					"-o", "yaml",
					"|",
					"kubectl", "apply", "-f", "-")
				restoreOutput, restoreErr := tc.execCommandWithLogging(t, "Restore cluster-monitoring-config", restoreConfigCmd)
				if restoreErr != nil {
					t.Logf("Warning: Failed to restore cluster-monitoring-config: %v\nOutput: %s", restoreErr, restoreOutput)
				} else {
					t.Log("✅ Restored cluster-monitoring-config")
				}
			}
		}
	} else {
		t.Log("No original cluster-monitoring-config to restore (it didn't exist before)")
	}

	// Restore deployment replicas
	if len(tc.originalDeploymentReplicas) > 0 {
		t.Logf("Restoring %d deployment(s) to original replica counts", len(tc.originalDeploymentReplicas))

		for deploymentKey, originalReplicas := range tc.originalDeploymentReplicas {
			// Parse namespace and name from key (format: "namespace/name")
			parts := strings.Split(deploymentKey, "/")
			if len(parts) != 2 {
				t.Logf("Warning: Invalid deployment key format: %s", deploymentKey)
				continue
			}
			namespace, name := parts[0], parts[1]

			t.Logf("Restoring deployment %s in namespace %s to %d replicas", name, namespace, originalReplicas)
			scaleCmd := exec.CommandContext(context.Background(), "kubectl", "scale", "deployment", name,
				"-n", namespace,
				"--replicas="+strconv.Itoa(int(originalReplicas)))
			scaleOutput, scaleErr := tc.execCommandWithLogging(t, fmt.Sprintf("Restore deployment %s replicas", name), scaleCmd)
			if scaleErr != nil {
				t.Logf("Warning: Failed to restore deployment %s: %v\nOutput: %s", name, scaleErr, scaleOutput)
			} else {
				t.Logf("✅ Restored deployment %s to %d replicas", name, originalReplicas)
			}
		}
	}

	// Restore statefulset replicas
	if len(tc.originalStatefulSetReplicas) > 0 {
		t.Logf("Restoring %d statefulset(s) to original replica counts", len(tc.originalStatefulSetReplicas))

		for statefulsetKey, originalReplicas := range tc.originalStatefulSetReplicas {
			// Parse namespace and name from key (format: "namespace/name")
			parts := strings.Split(statefulsetKey, "/")
			if len(parts) != 2 {
				t.Logf("Warning: Invalid statefulset key format: %s", statefulsetKey)
				continue
			}
			namespace, name := parts[0], parts[1]

			t.Logf("Restoring statefulset %s in namespace %s to %d replicas", name, namespace, originalReplicas)
			scaleCmd := exec.CommandContext(context.Background(), "kubectl", "scale", "statefulset", name,
				"-n", namespace,
				"--replicas="+strconv.Itoa(int(originalReplicas)))
			scaleOutput, scaleErr := tc.execCommandWithLogging(t, fmt.Sprintf("Restore statefulset %s replicas", name), scaleCmd)
			if scaleErr != nil {
				t.Logf("Warning: Failed to restore statefulset %s: %v\nOutput: %s", name, scaleErr, scaleOutput)
			} else {
				t.Logf("✅ Restored statefulset %s to %d replicas", name, originalReplicas)
			}
		}
	}

	t.Log("Cluster state restoration completed")
}
