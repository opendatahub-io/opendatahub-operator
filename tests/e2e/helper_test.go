package e2e_test

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	gTypes "github.com/onsi/gomega/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	modelregistryctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

// Namespace and Operator Constants.
const (

	// Component API field name constants for v1 <-> v2 conversion.
	dataSciencePipelinesKind          = "DataSciencePipelines" // Kind name for DataSciencePipelines component
	dataSciencePipelinesComponentName = "datasciencepipelines" // v1 API component name for DataSciencePipelines
	aiPipelinesFieldName              = "aipipelines"          // v2 API field name for DataSciencePipelines component

	// Test timing constants.
	// controllerCacheRefreshDelay is the time to wait for controller-runtime
	// informer cache to update after resource deletion. This prevents cache
	// staleness issues in deletion/recreation tests.
	controllerCacheRefreshDelay = 5 * time.Second

	// Error tag constants for circuit breaker pattern.
	errorTagInfrastructure = "[INFRASTRUCTURE]"
	errorTagComponent      = "[COMPONENT]"
	errorTagController     = "[CONTROLLER]"

	// Operators constants.
	defaultOperatorChannel      = "stable"                                   // The default channel to install/check operators
	kueueOpName                 = "kueue-operator"                           // Name of the Kueue Operator
	certManagerOpName           = "openshift-cert-manager-operator"          // Name of the cert-manager Operator
	certManagerOpNamespace      = "cert-manager-operator"                    // Name of the cert-manager Namespace
	certManagerOpChannel        = "stable-v1"                                // Name of cert-manager operator stable channel
	jobSetOpName                = "job-set"                                  // Name of the JobSet Operator
	jobSetOpNamespace           = "openshift-jobset-operator"                // Namespace for the JobSet Operator
	jobSetOpChannel             = "tech-preview-v0.1"                        // Name of the JobSet Operator stable channel
	openshiftOperatorsNamespace = "openshift-operators"                      // Namespace for OpenShift Operators
	observabilityOpName         = "cluster-observability-operator"           // Name of the Cluster Observability Operator
	observabilityOpNamespace    = "openshift-cluster-observability-operator" // Namespace for the Cluster Observability Operator
	tempoOpName                 = "tempo-product"                            // Name of the Tempo Operator
	tempoOpNamespace            = "openshift-tempo-operator"                 // Namespace for the Tempo Operator
	opentelemetryOpName         = "opentelemetry-product"                    // Name of the OpenTelemetry Operator
	opentelemetryOpNamespace    = "openshift-opentelemetry-operator"         // Namespace for the OpenTelemetry Operator
	controllerDeploymentODH     = "opendatahub-operator-controller-manager"  // Name of the ODH deployment
	controllerDeploymentRhoai   = "rhods-operator"                           // Name of the Rhoai deployment
	leaderWorkerSetOpName       = "leader-worker-set"                        // Name of the Leader Worker Set Operator
	leaderWorkerSetNamespace    = "openshift-lws-operator"                   // Namespace for the Leader Worker Set Operator
	leaderWorkerSetChannel      = "stable-v1.0"                              // Channel for the Leader Worker Set Operator
	kueueOcpOperatorNamespace   = "openshift-kueue-operator"                 // Namespace for the OCP Kueue Operator
	kueueOcpOperatorChannel     = "stable-v1.2"                              // Channel for the OCP Kueue Operator
	kuadrantOpName              = "rhcl-operator"                            // Name of the Red Hat Connectivity Link Operator subscription.
	kuadrantNamespace           = "kuadrant-system"                          // Namespace for the Red Hat Connectivity Link Operator.
	dashboardRouteNameODH       = "odh-dashboard"                            // Name of the ODH dashboard route
	dashboardRouteNameRhoai     = "rhods-dashboard"                          // Name of the Rhoai dashboard route

)

// Configuration and Miscellaneous Constants.
const (
	ownedNamespaceNumber = 1 // Number of namespaces owned, adjust to 4 for RHOAI deployment

	dsciInstanceName = "e2e-test-dsci" // Instance name for the DSCInitialization
	dscInstanceName  = "e2e-test-dsc"  // Instance name for the DataScienceCluster

	// Standard error messages format.
	resourceNotNilErrorMsg       = "Expected a non-nil resource object but got nil."
	resourceNotFoundErrorMsg     = "Expected resource '%s' of kind '%s' to exist, but it was not found or could not be retrieved."
	resourceFoundErrorMsg        = "Expected resource '%s' of kind '%s' to not exist, but it was found."
	resourceEmptyErrorMsg        = "Expected resource list '%s' of kind '%s' to contain resources, but it was empty."
	resourceListNotEmptyErrorMsg = "Expected resource list '%s' of kind '%s' to be empty, but it contains resources."
	resourceFetchErrorMsg        = "Error occurred while fetching the resource '%s' of kind '%s': %v"
	unexpectedErrorMismatchMsg   = "Expected error '%v' to match the actual error '%v' for resource of kind '%s'."
)

// TestCaseOpts defines a function type that can be used to modify how individual test cases are executed.
type TestCaseOpts func(t *testing.T)

// ProgressTracker tracks progress for long-running operations and logs periodic updates.
// Use this inside Eventually() polling functions to provide visibility during long waits.
type ProgressTracker struct {
	startTime     time.Time
	lastLogTime   time.Time
	logInterval   time.Duration
	operationDesc string
	logFunc       func(format string, args ...interface{})
}

// NewProgressTracker creates a new progress tracker for logging periodic updates.
// operationDesc describes what operation is being waited for (e.g., "Kueue state transition to Removed").
// logFunc is the logging function to use (e.g., t.Logf or tc.Logf).
func NewProgressTracker(operationDesc string, logFunc func(format string, args ...interface{})) *ProgressTracker {
	now := time.Now()
	return &ProgressTracker{
		startTime:     now,
		lastLogTime:   now,
		logInterval:   30 * time.Second, // Log every 30 seconds
		operationDesc: operationDesc,
		logFunc:       logFunc,
	}
}

// LogProgress logs a progress update if enough time has elapsed since the last log.
// Call this inside your Eventually() polling function to get periodic progress updates.
// Returns true if a log message was printed, false otherwise.
func (pt *ProgressTracker) LogProgress() bool {
	now := time.Now()
	elapsed := now.Sub(pt.startTime)
	timeSinceLastLog := now.Sub(pt.lastLogTime)

	if timeSinceLastLog >= pt.logInterval {
		pt.logFunc("[PROGRESS] Still waiting for %s... (elapsed: %v)", pt.operationDesc, elapsed.Round(time.Second))
		pt.lastLogTime = now
		return true
	}
	return false
}

// LogFinal logs the final completion message with total elapsed time.
// Call this after the Eventually() succeeds or fails.
func (pt *ProgressTracker) LogFinal(success bool) {
	elapsed := time.Since(pt.startTime).Round(time.Second)
	if success {
		pt.logFunc("[PROGRESS] ✓ Completed: %s (total time: %v)", pt.operationDesc, elapsed)
	} else {
		pt.logFunc("[PROGRESS] ✗ Failed: %s (total time: %v)", pt.operationDesc, elapsed)
	}
}

// RunTestCases runs a series of test cases, optionally in parallel based on the provided options.
//
// Parameters:
//   - t (*testing.T): The test context passed into the test function.
//   - testCases ([]TestCase): A slice of test cases to execute.
//   - opts (...TestCaseOpts): Optional configuration options, like enabling parallel execution.
func RunTestCases(t *testing.T, testCases []TestCase, opts ...TestCaseOpts) {
	t.Helper()

	// Apply all provided options (e.g., parallel execution) to each test case.
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			// Set up panic handler for each individual test (must be first defer)
			defer HandleGlobalPanic()

			// Check for test failure and run diagnostics (only for failures, not panics)
			defer func() {
				if t.Failed() {
					HandleTestFailure(testCase.name)
				}
			}()

			// Apply each option to the current test
			for _, opt := range opts {
				opt(t)
			}

			// Run the test function for the current test case
			testCase.testFn(t)
		})
	}
}

// WithParallel is an option that marks test cases to run in parallel.
func WithParallel() TestCaseOpts {
	return func(t *testing.T) {
		t.Helper()

		t.Parallel() // Marks the test case to run in parallel with other tests
	}
}

// ExtractAndExpectValue extracts a value from an input using a jq expression and asserts conditions on it.
//
// This function extracts a value of type T from the given input using the specified jq expression.
// It ensures that extraction succeeds and applies one or more assertions to validate the extracted value.
//
// Parameters:
//   - g (Gomega): The Gomega testing instance used for assertions.
//   - in (any): The input data (e.g., a Kubernetes resource).
//   - expression (string): The jq expression used to extract a value from the input.
//   - matchers (GomegaMatcher): One or more Gomega matchers to validate the extracted value.
func ExtractAndExpectValue[T any](g Gomega, in any, expression string, matchers ...gTypes.GomegaMatcher) T {
	// Extract the value using the jq expression
	value, err := jq.ExtractValue[T](in, expression)

	// Expect no errors during extraction
	g.Expect(err).NotTo(HaveOccurred(), "Failed to extract value using expression: %s", expression)

	// Apply matchers to validate the extracted value
	g.Expect(value).To(And(matchers...), "Extracted value from %s does not match expected conditions", expression)

	return value
}

// CreateDSCI creates a DSCInitialization CR.
func CreateDSCI(name, appNamespace, monitoringNamespace string) *dsciv2.DSCInitialization {
	return &dsciv2.DSCInitialization{
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.DSCInitialization.Kind,
			APIVersion: gvk.DSCInitialization.GroupVersion().String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: dsciv2.DSCInitializationSpec{
			ApplicationsNamespace: appNamespace,
			Monitoring: serviceApi.DSCIMonitoring{
				ManagementSpec: common.ManagementSpec{
					ManagementState: operatorv1.Removed, // keep rhoai branch to Managed so we can test it
				},
				MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{
					Namespace: monitoringNamespace,
				},
			},
			TrustedCABundle: &dsciv2.TrustedCABundleSpec{
				ManagementState: operatorv1.Managed,
				CustomCABundle:  "",
			},
		},
	}
}

// CreateDSCIv1 creates a DSCInitialization v1 CR.
func CreateDSCIv1(name, appNamespace, monitoringNamespace string) *dsciv1.DSCInitialization {
	return &dsciv1.DSCInitialization{
		TypeMeta: metav1.TypeMeta{
			Kind:       gvk.DSCInitializationV1.Kind,
			APIVersion: gvk.DSCInitializationV1.GroupVersion().String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: dsciv1.DSCInitializationSpec{
			ApplicationsNamespace: appNamespace,
			Monitoring: serviceApi.DSCIMonitoring{
				ManagementSpec: common.ManagementSpec{
					ManagementState: operatorv1.Removed, // keep rhoai branch to Managed so we can test it
				},
				MonitoringCommonSpec: serviceApi.MonitoringCommonSpec{
					Namespace: monitoringNamespace,
				},
			},
			TrustedCABundle: &dsciv1.TrustedCABundleSpec{
				ManagementState: operatorv1.Managed,
				CustomCABundle:  "",
			},
		},
	}
}

// CreateDSC creates a DataScienceCluster CR.
func CreateDSC(name string, workbenchesNamespace string) *dscv2.DataScienceCluster {
	return &dscv2.DataScienceCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DataScienceCluster",
			APIVersion: dscv2.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: dscv2.DataScienceClusterSpec{
			Components: dscv2.Components{
				// keep dashboard as enabled, because other test is rely on this
				Dashboard: componentApi.DSCDashboard{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				Workbenches: componentApi.DSCWorkbenches{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					WorkbenchesCommonSpec: componentApi.WorkbenchesCommonSpec{
						WorkbenchNamespace: workbenchesNamespace,
					},
				},
				AIPipelines: componentApi.DSCDataSciencePipelines{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{},
				},
				Ray: componentApi.DSCRay{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				Kueue: componentApi.DSCKueue{
					KueueManagementSpec: componentApi.KueueManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				TrustyAI: componentApi.DSCTrustyAI{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				ModelRegistry: componentApi.DSCModelRegistry{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					ModelRegistryCommonSpec: componentApi.ModelRegistryCommonSpec{
						RegistriesNamespace: modelregistryctrl.DefaultModelRegistriesNamespace,
					},
				},
				TrainingOperator: componentApi.DSCTrainingOperator{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				Trainer: componentApi.DSCTrainer{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				FeastOperator: componentApi.DSCFeastOperator{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				LlamaStackOperator: componentApi.DSCLlamaStackOperator{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				MLflowOperator: componentApi.DSCMLflowOperator{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
			},
		},
	}
}

func CreateDSCv1(name string, workbenchesNamespace string) *dscv1.DataScienceCluster {
	return &dscv1.DataScienceCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DataScienceCluster",
			APIVersion: dscv1.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: dscv1.DataScienceClusterSpec{
			Components: dscv1.Components{
				Dashboard: componentApi.DSCDashboard{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				Workbenches: componentApi.DSCWorkbenches{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					WorkbenchesCommonSpec: componentApi.WorkbenchesCommonSpec{
						WorkbenchNamespace: workbenchesNamespace,
					},
				},
				ModelMeshServing: componentApi.DSCModelMeshServing{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				DataSciencePipelines: componentApi.DSCDataSciencePipelines{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				Kserve: componentApi.DSCKserve{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					KserveCommonSpec: componentApi.KserveCommonSpec{},
				},
				CodeFlare: componentApi.DSCCodeFlare{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				Ray: componentApi.DSCRay{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				TrustyAI: componentApi.DSCTrustyAI{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				ModelRegistry: componentApi.DSCModelRegistry{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
					ModelRegistryCommonSpec: componentApi.ModelRegistryCommonSpec{
						RegistriesNamespace: modelregistryctrl.DefaultModelRegistriesNamespace,
					},
				},
				TrainingOperator: componentApi.DSCTrainingOperator{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				LlamaStackOperator: componentApi.DSCLlamaStackOperator{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				FeastOperator: componentApi.DSCFeastOperator{
					ManagementSpec: common.ManagementSpec{
						ManagementState: operatorv1.Removed,
					},
				},
				Kueue: dscv1.DSCKueueV1{
					KueueManagementSpecV1: dscv1.KueueManagementSpecV1{
						ManagementState: operatorv1.Removed,
					},
				},
			},
		},
	}
}

func CreateHardwareProfile(name, namespace, apiVersion string) *unstructured.Unstructured {
	minCount := intstr.FromInt32(1)
	maxCount := intstr.FromInt32(4)
	defaultCount := intstr.FromInt32(2)

	hwProfile := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": apiVersion,
			"kind":       "HardwareProfile",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]interface{}{
				"identifiers": []map[string]interface{}{
					{
						"displayName":  "GPU",
						"identifier":   "nvidia.com/gpu",
						"minCount":     minCount.IntVal,
						"maxCount":     maxCount.IntVal,
						"defaultCount": defaultCount.IntVal,
						"resourceType": "Accelerator",
					},
				},
				"scheduling": map[string]interface{}{
					"type": "Node",
					"node": map[string]interface{}{
						"nodeSelector": map[string]interface{}{
							"kubernetes.io/arch":             "amd64",
							"node-role.kubernetes.io/worker": "",
						},
						"tolerations": []map[string]interface{}{
							{
								"key":      "nvidia.com/gpu",
								"operator": "Exists",
								"effect":   "NoSchedule",
							},
						},
					},
				},
			},
		},
	}

	return hwProfile
}

// CreateJobSetOperator creates a JobSetOperator CR.
func CreateJobSetOperator() *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "operator.openshift.io/v1",
			"kind":       "JobSetOperator",
			"metadata": map[string]interface{}{
				"name": "cluster",
			},
			"spec": map[string]interface{}{
				"logLevel":         "Normal",
				"operatorLogLevel": "Normal",
			},
		},
	}
}

// CreateNamespaceWithLabels creates a namespace manifest with optional labels for use with WithObjectToCreate.
func CreateNamespaceWithLabels(name string, labels map[string]string) *corev1.Namespace {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}

	if len(labels) > 0 {
		ns.Labels = labels
	}

	return ns
}

// defaultErrorMessageIfNone appends a default message to args if args is empty.
// It formats the default message using the provided format and formatArgs.
func defaultErrorMessageIfNone(format string, formatArgs []any, args []any) []any {
	// If no custom args are provided, append the default message
	if len(args) == 0 {
		args = append(args, fmt.Sprintf(format, formatArgs...))
	}
	return args
}

// ParseTestFlags Parses go test flags separately because pflag package ignores flags with '-test.' prefix
// Related issues:
// https://github.com/spf13/pflag/issues/63
// https://github.com/spf13/pflag/issues/238
func ParseTestFlags() error {
	var testFlags []string
	for _, f := range os.Args[1:] {
		if strings.HasPrefix(f, "-test.") {
			testFlags = append(testFlags, f)
		}
	}
	return flag.CommandLine.Parse(testFlags)
}

// getControllerDeploymentName returns deployment name based on platform.
func (tc *TestContext) getControllerDeploymentName() string {
	platform := tc.FetchPlatformRelease()
	return getControllerDeploymentNameByPlatform(platform)
}

func getControllerDeploymentNameByPlatform(platform common.Platform) string {
	switch platform {
	case cluster.SelfManagedRhoai, cluster.ManagedRhoai:
		return controllerDeploymentRhoai
	case cluster.OpenDataHub:
		return controllerDeploymentODH
	default:
		return controllerDeploymentODH
	}
}

func getDashboardRouteNameByPlatform(platform common.Platform) string {
	switch platform {
	case cluster.SelfManagedRhoai, cluster.ManagedRhoai:
		return dashboardRouteNameRhoai
	case cluster.OpenDataHub:
		return dashboardRouteNameODH
	default:
		return dashboardRouteNameODH
	}
}

// diagnoseOperatorDeploymentFailure performs deep diagnostics when operator deployment has 0 ready replicas.
// This provides actionable information to eliminate 30-60 minutes of manual debugging.
func diagnoseOperatorDeploymentFailure(tc *TestContext, deploymentName string) error {
	tc.Logf("[FAIL-FAST] ⚠ No ready replicas for deployment %s - running deep diagnostics...", deploymentName)

	// List pods for this deployment
	podList := &corev1.PodList{}
	labelSelector := client.MatchingLabels{"app": "opendatahub-operator"}
	if err := tc.Client().List(tc.Context(), podList, client.InNamespace(tc.OperatorNamespace), labelSelector); err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	if len(podList.Items) == 0 {
		tc.Logf("[FAIL-FAST] ✗ NO PODS FOUND - deployment may not have created pods yet")
		return nil
	}

	tc.Logf("[FAIL-FAST] Found %d pod(s) for deployment", len(podList.Items))

	for i, pod := range podList.Items {
		tc.Logf("[FAIL-FAST] Analyzing pod %d/%d: %s", i+1, len(podList.Items), pod.Name)
		tc.Logf("[FAIL-FAST]   Phase: %s", pod.Status.Phase)

		// Analyze pod conditions
		for _, condition := range pod.Status.Conditions {
			if condition.Status != corev1.ConditionTrue {
				tc.Logf("[FAIL-FAST]   Condition %s: %s - %s", condition.Type, condition.Status, condition.Message)
			}
		}

		// Check if pod is pending due to scheduling issues
		if pod.Status.Phase == corev1.PodPending {
			tc.Logf("[FAIL-FAST]   ⚠ POD IS PENDING - checking for scheduling issues...")
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodScheduled && condition.Status == corev1.ConditionFalse {
					tc.Logf("[FAIL-FAST]   ✗ NOT SCHEDULED: %s", condition.Message)
					if strings.Contains(condition.Message, "Insufficient") {
						tc.Logf("[FAIL-FAST]   ⚠ INSUFFICIENT RESOURCES - cluster needs more capacity")
					}
				}
			}
		}

		// Analyze container statuses
		for _, containerStatus := range pod.Status.ContainerStatuses {
			tc.Logf("[FAIL-FAST]   Container %s:", containerStatus.Name)
			tc.Logf("[FAIL-FAST]     Ready: %v, RestartCount: %d", containerStatus.Ready, containerStatus.RestartCount)

			// Detect crash loops
			if containerStatus.RestartCount > 0 {
				tc.Logf("[FAIL-FAST]     ⚠ Container has restarted %d times - CRASH LOOP DETECTED", containerStatus.RestartCount)
			}

			// Check container state
			if containerStatus.State.Waiting != nil {
				waiting := containerStatus.State.Waiting
				tc.Logf("[FAIL-FAST]     ✗ WAITING: %s - %s", waiting.Reason, waiting.Message)

				// Categorize common waiting reasons
				switch waiting.Reason {
				case "ImagePullBackOff", "ErrImagePull":
					tc.Logf("[FAIL-FAST]     ⚠ IMAGE PULL ISSUE - check image name and registry access")
				case "CrashLoopBackOff":
					tc.Logf("[FAIL-FAST]     ⚠ CRASH LOOP - container is repeatedly crashing")
				case "CreateContainerError":
					tc.Logf("[FAIL-FAST]     ⚠ CONTAINER CREATION ERROR - check container config")
				}
			}

			if containerStatus.State.Terminated != nil {
				terminated := containerStatus.State.Terminated
				tc.Logf("[FAIL-FAST]     ✗ TERMINATED: Reason=%s, ExitCode=%d", terminated.Reason, terminated.ExitCode)
				if terminated.Message != "" {
					tc.Logf("[FAIL-FAST]     Message: %s", terminated.Message)
				}
			}

			// Get logs from previous container if it crashed
			if containerStatus.RestartCount > 0 {
				tc.Logf("[FAIL-FAST]   Attempting to fetch logs from previous container instance...")
				// Note: We can't easily get logs without additional client setup, but we log the attempt
				// In a full implementation, this would use tc.Clientset().CoreV1().Pods().GetLogs()
				tc.Logf("[FAIL-FAST]   (Log collection requires additional setup - check pod logs manually for: %s/%s)", pod.Name, containerStatus.Name)
			}
		}

		// Get recent events for this pod
		tc.Logf("[FAIL-FAST]   Checking recent events for pod...")
		eventList := &corev1.EventList{}
		fieldSelector := client.MatchingFields{"involvedObject.name": pod.Name}
		if err := tc.Client().List(tc.Context(), eventList, client.InNamespace(tc.OperatorNamespace), fieldSelector); err != nil {
			tc.Logf("[FAIL-FAST]   Failed to get events: %v", err)
		} else {
			eventCount := 0
			for _, event := range eventList.Items {
				// Show last 5 events
				if eventCount >= 5 {
					break
				}
				tc.Logf("[FAIL-FAST]     %s: %s - %s", event.Type, event.Reason, event.Message)
				eventCount++
			}
			if eventCount == 0 {
				tc.Logf("[FAIL-FAST]   No recent events found")
			}
		}
	}

	return nil
}

// InfrastructureHealthCheck runs quick checks to verify cluster is ready for e2e tests.
// This is a fail-fast mechanism to detect infrastructure issues in < 5 minutes instead of
// running for 90+ minutes before timeout. Based on CI audit data showing 87.6% of failures
// are infrastructure-related, not code bugs.
//
// Returns error if infrastructure is degraded, allowing tests to fail fast and save CI time.
func InfrastructureHealthCheck(tc *TestContext) error {
	tc.Logf("[FAIL-FAST] Running infrastructure health check (pre-flight validation)...")

	// Check 1: Cluster nodes are ready
	tc.Logf("[FAIL-FAST] Checking cluster nodes are ready...")
	nodeList := &corev1.NodeList{}
	if err := tc.Client().List(tc.Context(), nodeList); err != nil {
		return fmt.Errorf("[INFRASTRUCTURE] failed to list nodes: %w", err)
	}

	readyNodes := 0
	totalNodes := len(nodeList.Items)
	for _, node := range nodeList.Items {
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
				readyNodes++
				break
			}
		}
	}

	if readyNodes == 0 {
		return fmt.Errorf("[INFRASTRUCTURE] no ready nodes found (0/%d) - cluster is not operational", totalNodes)
	}

	// Warn but don't fail if some nodes are not ready (cluster may still be usable)
	if readyNodes < totalNodes {
		tc.Logf("[FAIL-FAST] WARNING: Only %d/%d nodes are ready - cluster may be degraded", readyNodes, totalNodes)
	} else {
		tc.Logf("[FAIL-FAST] ✓ All %d nodes are ready", readyNodes)
	}

	// Check 2: Operator pod is running
	// Try both common operator deployment names (we can't use FetchPlatformRelease() here as it may require
	// DSCInitialization to exist, which creates a chicken-and-egg problem)
	tc.Logf("[FAIL-FAST] Checking operator deployment is ready...")
	deploymentNames := []string{controllerDeploymentODH, controllerDeploymentRhoai}

	foundDeployment := false
	for _, deploymentName := range deploymentNames {
		deployment := &unstructured.Unstructured{}
		deployment.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "apps",
			Version: "v1",
			Kind:    "Deployment",
		})

		err := tc.Client().Get(tc.Context(), types.NamespacedName{
			Name:      deploymentName,
			Namespace: tc.OperatorNamespace,
		}, deployment)

		if err == nil {
			// Found a deployment, check its readiness
			foundDeployment = true
			readyReplicas, found, replicasErr := unstructured.NestedInt64(deployment.Object, "status", "readyReplicas")
			switch {
			case replicasErr != nil || !found:
				tc.Logf("[FAIL-FAST] WARNING: Could not determine operator pod readiness for %s", deploymentName)
			case readyReplicas == 0:
				// DEEP DIAGNOSTICS: Investigate why operator has 0 ready replicas
				if diagErr := diagnoseOperatorDeploymentFailure(tc, deploymentName); diagErr != nil {
					tc.Logf("[FAIL-FAST] ⚠ Diagnostic error: %v", diagErr)
				}
				return fmt.Errorf("[INFRASTRUCTURE] operator deployment %s has 0 ready replicas - operator not running", deploymentName)
			default:
				tc.Logf("[FAIL-FAST] ✓ Operator deployment %s has %d ready replica(s)", deploymentName, readyReplicas)
			}
			break
		}
	}

	if !foundDeployment {
		return fmt.Errorf("[INFRASTRUCTURE] operator deployment not found - tried: %v in namespace %s",
			deploymentNames, tc.OperatorNamespace)
	}

	// Check 3: API server is responsive (implicit from previous checks, but let's verify)
	tc.Logf("[FAIL-FAST] Checking API server responsiveness...")
	namespaceList := &corev1.NamespaceList{}
	if err := tc.Client().List(tc.Context(), namespaceList, &client.ListOptions{Limit: 1}); err != nil {
		return fmt.Errorf("[INFRASTRUCTURE] API server not responding to list requests: %w", err)
	}
	tc.Logf("[FAIL-FAST] ✓ API server is responsive")

	// Check 4: Required CRDs are installed
	tc.Logf("[FAIL-FAST] Checking required CRDs are installed...")
	requiredCRDs := []string{
		"dscinitializations.dscinitialization.opendatahub.io",
		"datascienceclusters.datasciencecluster.opendatahub.io",
	}

	for _, crdName := range requiredCRDs {
		crd := &unstructured.Unstructured{}
		crd.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "apiextensions.k8s.io",
			Version: "v1",
			Kind:    "CustomResourceDefinition",
		})

		if err := tc.Client().Get(tc.Context(), types.NamespacedName{Name: crdName}, crd); err != nil {
			return fmt.Errorf("[INFRASTRUCTURE] required CRD %s not found - operator may not be installed correctly: %w", crdName, err)
		}

		tc.Logf("[FAIL-FAST] ✓ CRD %s is installed", crdName)
	}

	tc.Logf("[FAIL-FAST] ✓ Infrastructure health check PASSED - cluster is ready for e2e tests")
	return nil
}

// EventuallyWithCircuitBreaker wraps Gomega Eventually with a circuit breaker pattern.
// If the condition fails consecutively failureThreshold times, it fails fast with an
// [INFRASTRUCTURE] error instead of waiting for the full timeout.
//
// This pattern helps detect persistent infrastructure failures quickly instead of
// waiting 10+ minutes for timeout. Based on CI audit showing average 92-minute
// failed test duration.
//
// Example usage:
//
//	EventuallyWithCircuitBreaker(tc.g, func() error {
//	    return waitForDeployment(ctx, "odh-dashboard")
//	}, 5*time.Minute, 10*time.Second, 10, "dashboard deployment")
func EventuallyWithCircuitBreaker(
	g Gomega,
	condition func() error,
	timeout time.Duration,
	pollInterval time.Duration,
	failureThreshold int,
	description string,
) {
	consecutiveFailures := 0
	var lastErr error

	g.Eventually(func() error {
		err := condition()
		if err != nil {
			consecutiveFailures++
			lastErr = err

			// Circuit breaker: fail fast if we hit the threshold
			if consecutiveFailures >= failureThreshold {
				return fmt.Errorf("[INFRASTRUCTURE] %s failing consistently after %d attempts - likely infrastructure issue: %w",
					description, consecutiveFailures, lastErr)
			}
		} else {
			// Reset counter on success
			consecutiveFailures = 0
		}
		return err
	}, timeout, pollInterval).Should(Succeed(),
		"Condition '%s' did not succeed within %v", description, timeout)
}

// classifyError categorizes errors for circuit breaker pattern to enable proper error tagging
// and auto-retry logic. Returns the appropriate error tag prefix and a boolean indicating
// whether this error should count towards circuit breaker threshold.
//
// Error categories:
//   - [INFRASTRUCTURE]: Platform/cluster issues outside developer control (auto-retry candidates)
//   - [COMPONENT]: Application-level issues that might be transient (consider retry)
//   - [CONTROLLER]: Operator controller issues (may need investigation)
//   - No tag: Configuration or test issues (don't count toward circuit breaker)
//
// Based on CI audit data showing 87.6% of failures are infrastructure-related.
func classifyError(err error) (string, bool) {
	if err == nil {
		return "", false
	}

	errMsg := err.Error()

	// Infrastructure errors - platform/cluster issues
	// These are the most common (87.6% of failures) and should trigger auto-retry
	switch {
	case strings.Contains(errMsg, "ImagePullBackOff"),
		strings.Contains(errMsg, "ErrImagePull"),
		strings.Contains(errMsg, "image pull"),
		strings.Contains(errMsg, "manifest unknown"),
		strings.Contains(errMsg, "registry"):
		return errorTagInfrastructure, true

	case strings.Contains(errMsg, "nodes are available"),
		strings.Contains(errMsg, "Insufficient"),
		strings.Contains(errMsg, "node(s)"),
		strings.Contains(errMsg, "Unschedulable"):
		return errorTagInfrastructure, true

	case strings.Contains(errMsg, "failed to mount"),
		strings.Contains(errMsg, "Volume"),
		strings.Contains(errMsg, "PersistentVolumeClaim"),
		strings.Contains(errMsg, "storage"):
		return errorTagInfrastructure, true

	case strings.Contains(errMsg, "context deadline exceeded"),
		strings.Contains(errMsg, "i/o timeout"),
		strings.Contains(errMsg, "connection refused"),
		strings.Contains(errMsg, "EOF"):
		return errorTagInfrastructure, true

	case strings.Contains(errMsg, "apiserver"),
		strings.Contains(errMsg, "etcd"),
		strings.Contains(errMsg, "control plane"):
		return errorTagInfrastructure, true

	// Component errors - application-level issues
	case strings.Contains(errMsg, "CrashLoopBackOff"),
		strings.Contains(errMsg, "Error: "),
		strings.Contains(errMsg, "panic"):
		return errorTagComponent, true

	case strings.Contains(errMsg, "config"),
		strings.Contains(errMsg, "validation failed"),
		strings.Contains(errMsg, "invalid"):
		return errorTagComponent, true

	case strings.Contains(errMsg, "ResourceQuota"),
		strings.Contains(errMsg, "LimitRange"),
		strings.Contains(errMsg, "forbidden"):
		return errorTagComponent, true

	// Controller errors - operator/reconciler issues
	case strings.Contains(errMsg, "reconcile"),
		strings.Contains(errMsg, "controller"),
		strings.Contains(errMsg, "finalizer"):
		return errorTagController, true

	// Default: unclassified errors don't get tagged but still count
	// This ensures we fail fast on persistent unknown issues
	default:
		return "", true
	}
}

// diagnoseDeletionRecoveryFailure collects comprehensive diagnostics when a resource
// deletion recovery test fails. This helps investigate why the controller failed to
// recreate the resource within the expected timeout.
//
// Based on CI audit data showing ConfigMap deletion recovery has 73% pass rate (26.7% failure):
// - Passing runs: 5-13 seconds (controller recreates quickly)
// - Failing runs: 605-611 seconds (timeout, controller never recreates)
//
// This function investigates potential root causes:
// 1. Controller pod health (crashes, restarts, OOMKilled)
// 2. Controller resource pressure (CPU/memory throttling)
// 3. Controller rate limiting or backoff
// 4. Cluster resource exhaustion
// 5. OwnerReference issues blocking recreation.
func diagnoseDeletionRecoveryFailure(
	tc *TestContext,
	resourceGVK schema.GroupVersionKind,
	resourceName string,
	resourceNamespace string,
	componentKind string,
) {
	tc.Logf("\n=== DELETION RECOVERY DIAGNOSTICS: %s/%s (component: %s) ===\n",
		resourceGVK.Kind, resourceName, componentKind)

	diagnoseControllerPodHealth(tc)
	diagnoseResourceExistence(tc, resourceGVK, resourceName, resourceNamespace)
	diagnoseComponentStatus(tc, componentKind)
	diagnoseRecentEvents(tc, resourceName, resourceNamespace, componentKind)

	tc.Logf("\n=== END DELETION RECOVERY DIAGNOSTICS ===\n")
}

// diagnoseControllerPodHealth checks controller deployment and pod health.
func diagnoseControllerPodHealth(tc *TestContext) {
	tc.Logf("\n--- Controller Pod Health ---")

	// Try both common operator deployment names
	deploymentNames := []string{controllerDeploymentODH, controllerDeploymentRhoai}

	for _, depName := range deploymentNames {
		deployment := &unstructured.Unstructured{}
		deployment.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "apps",
			Version: "v1",
			Kind:    "Deployment",
		})

		err := tc.Client().Get(tc.Context(), types.NamespacedName{
			Name:      depName,
			Namespace: tc.OperatorNamespace,
		}, deployment)

		if err != nil {
			if client.IgnoreNotFound(err) == nil {
				tc.Logf("⚠️  Deployment %s not found (trying alternate name)", depName)
			} else {
				tc.Logf("⚠️  Error fetching deployment %s: %v", depName, err)
			}
			continue
		}

		logDeploymentStatus(tc, deployment, depName)
		logControllerPods(tc)
	}
}

// logDeploymentStatus logs controller deployment status.
func logDeploymentStatus(tc *TestContext, deployment *unstructured.Unstructured, depName string) {
	readyReplicas, _, _ := unstructured.NestedInt64(deployment.Object, "status", "readyReplicas")
	replicas, _, _ := unstructured.NestedInt64(deployment.Object, "status", "replicas")
	unavailableReplicas, _, _ := unstructured.NestedInt64(deployment.Object, "status", "unavailableReplicas")

	tc.Logf("Operator Deployment: %s", depName)
	tc.Logf("  Ready Replicas: %d/%d", readyReplicas, replicas)
	if unavailableReplicas > 0 {
		tc.Logf("  ⚠️  Unavailable Replicas: %d", unavailableReplicas)
	}
}

// logControllerPods lists and analyzes controller pods.
func logControllerPods(tc *TestContext) {
	podList := &corev1.PodList{}
	labelSelector := client.MatchingLabels{"control-plane": "controller-manager"}
	if err := tc.Client().List(tc.Context(), podList, client.InNamespace(tc.OperatorNamespace), labelSelector); err != nil {
		tc.Logf("  ⚠️  Failed to list controller pods: %v", err)
		return
	}

	for _, pod := range podList.Items {
		logPodStatus(tc, &pod)
	}
}

// logPodStatus logs detailed pod status including containers and resources.
func logPodStatus(tc *TestContext, pod *corev1.Pod) {
	tc.Logf("\n  Pod: %s", pod.Name)
	tc.Logf("    Phase: %s", pod.Status.Phase)
	tc.Logf("    Restarts: %d", getPodRestartCount(pod))

	if pod.Status.Phase != corev1.PodRunning {
		tc.Logf("    ⚠️  Pod not running!")
	}

	// Check container statuses
	for _, containerStatus := range pod.Status.ContainerStatuses {
		logContainerStatus(tc, &containerStatus)
	}

	// Check resource requests/limits
	for _, container := range pod.Spec.Containers {
		if container.Name == "manager" || container.Name == "controller-manager" {
			logContainerResources(tc, &container)
		}
	}
}

// logContainerStatus logs container state and health.
func logContainerStatus(tc *TestContext, containerStatus *corev1.ContainerStatus) {
	tc.Logf("    Container: %s", containerStatus.Name)
	tc.Logf("      Ready: %t", containerStatus.Ready)
	tc.Logf("      RestartCount: %d", containerStatus.RestartCount)

	if containerStatus.State.Waiting != nil {
		tc.Logf("      ⚠️  Waiting: %s - %s",
			containerStatus.State.Waiting.Reason,
			containerStatus.State.Waiting.Message)
	}
	if containerStatus.State.Terminated != nil {
		tc.Logf("      ⚠️  Terminated: %s - %s (exit code: %d)",
			containerStatus.State.Terminated.Reason,
			containerStatus.State.Terminated.Message,
			containerStatus.State.Terminated.ExitCode)
	}

	// Check for OOMKilled
	if containerStatus.LastTerminationState.Terminated != nil {
		if containerStatus.LastTerminationState.Terminated.Reason == "OOMKilled" {
			tc.Logf("      ⚠️⚠️  Last termination: OOMKilled - controller may be under memory pressure!")
		}
	}
}

// logContainerResources logs container resource requests and limits.
func logContainerResources(tc *TestContext, container *corev1.Container) {
	tc.Logf("    Resource Limits:")
	if container.Resources.Limits != nil {
		cpu := container.Resources.Limits.Cpu()
		memory := container.Resources.Limits.Memory()
		tc.Logf("      CPU: %s", cpu.String())
		tc.Logf("      Memory: %s", memory.String())
	}
	tc.Logf("    Resource Requests:")
	if container.Resources.Requests != nil {
		cpu := container.Resources.Requests.Cpu()
		memory := container.Resources.Requests.Memory()
		tc.Logf("      CPU: %s", cpu.String())
		tc.Logf("      Memory: %s", memory.String())
	}
}

// diagnoseResourceExistence checks if the deleted resource still exists.
func diagnoseResourceExistence(tc *TestContext, resourceGVK schema.GroupVersionKind, resourceName, resourceNamespace string) {
	tc.Logf("\n--- Resource Existence Check ---")
	resource := &unstructured.Unstructured{}
	resource.SetGroupVersionKind(resourceGVK)
	err := tc.Client().Get(tc.Context(), types.NamespacedName{
		Name:      resourceName,
		Namespace: resourceNamespace,
	}, resource)

	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			tc.Logf("✓ Resource does NOT exist (deletion successful, recreation failed)")
		} else {
			tc.Logf("⚠️  Error checking resource existence: %v", err)
		}
		return
	}

	tc.Logf("⚠️  Resource STILL EXISTS - may not have been deleted properly")
	tc.Logf("  UID: %s", resource.GetUID())
	tc.Logf("  ResourceVersion: %s", resource.GetResourceVersion())
	tc.Logf("  DeletionTimestamp: %v", resource.GetDeletionTimestamp())

	if resource.GetDeletionTimestamp() != nil {
		tc.Logf("  ⚠️⚠️  Resource has DeletionTimestamp but still exists - finalizer may be stuck!")
		finalizers := resource.GetFinalizers()
		if len(finalizers) > 0 {
			tc.Logf("  Finalizers: %v", finalizers)
		}
	}
}

// diagnoseComponentStatus checks component CR status and conditions.
func diagnoseComponentStatus(tc *TestContext, componentKind string) {
	tc.Logf("\n--- Component Status Check ---")
	componentGVK := getComponentGVK(componentKind)
	component := &unstructured.Unstructured{}
	component.SetGroupVersionKind(componentGVK)
	componentName := getComponentInstanceName(componentKind)

	err := tc.Client().Get(tc.Context(), types.NamespacedName{Name: componentName}, component)
	if err != nil {
		tc.Logf("⚠️  Failed to get component %s: %v", componentKind, err)
		return
	}

	tc.Logf("Component %s:", componentKind)
	logComponentConditions(tc, component)
	logObservedGeneration(tc, component)
}

// logComponentConditions logs component status conditions.
func logComponentConditions(tc *TestContext, component *unstructured.Unstructured) {
	conditions, found, _ := unstructured.NestedSlice(component.Object, "status", "conditions")
	if !found || len(conditions) == 0 {
		tc.Logf("  ⚠️  No conditions found in status")
		return
	}

	tc.Logf("  Conditions:")
	for _, cond := range conditions {
		condMap, ok := cond.(map[string]interface{})
		if !ok {
			continue
		}
		condType, _ := condMap["type"].(string)
		condStatus, _ := condMap["status"].(string)
		condReason, _ := condMap["reason"].(string)
		condMessage, _ := condMap["message"].(string)

		statusIcon := "✓"
		if condStatus != "True" && condType == "Ready" {
			statusIcon = "⚠️ "
		}

		tc.Logf("    %s %s: %s", statusIcon, condType, condStatus)
		if condReason != "" {
			tc.Logf("      Reason: %s", condReason)
		}
		if condMessage != "" && condStatus != "True" {
			tc.Logf("      Message: %s", condMessage)
		}
	}
}

// logObservedGeneration checks if component controller is lagging.
func logObservedGeneration(tc *TestContext, component *unstructured.Unstructured) {
	observedGen, found, _ := unstructured.NestedInt64(component.Object, "status", "observedGeneration")
	if !found {
		return
	}

	generation := component.GetGeneration()
	if observedGen != generation {
		tc.Logf("  ⚠️⚠️  ObservedGeneration (%d) != Generation (%d) - controller may be lagging!",
			observedGen, generation)
	} else {
		tc.Logf("  ✓ ObservedGeneration matches Generation (%d)", observedGen)
	}
}

// diagnoseRecentEvents checks for relevant events in the last 5 minutes.
func diagnoseRecentEvents(tc *TestContext, resourceName, resourceNamespace, componentKind string) {
	tc.Logf("\n--- Recent Events (last 5 minutes) ---")
	eventList := &corev1.EventList{}
	if err := tc.Client().List(tc.Context(), eventList, client.InNamespace(resourceNamespace)); err != nil {
		tc.Logf("⚠️  Failed to list events: %v", err)
		return
	}

	relevantEvents := 0
	fiveMinutesAgo := metav1.Now().Add(-5 * time.Minute)

	for _, event := range eventList.Items {
		// Only show recent events
		if event.LastTimestamp.Time.Before(fiveMinutesAgo) {
			continue
		}

		// Filter for relevant events (controller, resource, or errors)
		if strings.Contains(event.InvolvedObject.Name, resourceName) ||
			strings.Contains(event.Reason, "Failed") ||
			strings.Contains(event.Reason, "Error") ||
			strings.Contains(event.Message, componentKind) ||
			event.Type == corev1.EventTypeWarning {
			relevantEvents++
			tc.Logf("  [%s] %s/%s: %s - %s",
				event.LastTimestamp.Format("15:04:05"),
				event.InvolvedObject.Kind,
				event.InvolvedObject.Name,
				event.Reason,
				event.Message)
		}
	}

	if relevantEvents == 0 {
		tc.Logf("  No relevant events in last 5 minutes")
	}
}

// Helper function to get pod restart count.
func getPodRestartCount(pod *corev1.Pod) int32 {
	var restarts int32
	for _, containerStatus := range pod.Status.ContainerStatuses {
		restarts += containerStatus.RestartCount
	}
	return restarts
}

// Helper function to get component GVK from kind string.
func getComponentGVK(componentKind string) schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   "components.opendatahub.io",
		Version: "v1alpha1",
		Kind:    componentKind,
	}
}

// Helper function to get component instance name from kind.
func getComponentInstanceName(componentKind string) string {
	// Most components use lowercase kind as instance name
	// ModelsAsService uses "modelsasservice"
	switch componentKind {
	case "ModelsAsService":
		return "modelsasservice"
	case "DataSciencePipelines":
		return "datasciencepipelines"
	case "Kserve":
		return "kserve"
	default:
		return strings.ToLower(componentKind)
	}
}
