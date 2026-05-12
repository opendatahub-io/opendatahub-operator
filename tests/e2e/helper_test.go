package e2e_test

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	gTypes "github.com/onsi/gomega/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	modelregistryctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/gateway"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
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

	// Operators constants.
	defaultOperatorChannel      = "stable"                                   // The default channel to install/check operators
	kueueOpName                 = "kueue-operator"                           // Name of the Kueue Operator
	certManagerOpName           = "openshift-cert-manager-operator"          // Name of the cert-manager Operator
	certManagerOpNamespace      = "cert-manager-operator"                    // Name of the cert-manager Namespace
	certManagerOpChannel        = "stable-v1"                                // Name of cert-manager operator stable channel
	jobSetOpName                = "job-set"                                  // Name of the JobSet Operator
	jobSetOpNamespace           = "openshift-jobset-operator"                // Namespace for the JobSet Operator
	jobSetOpChannel             = "stable-v1.0"                              // Name of the JobSet Operator stable channel
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

)

// Configuration and Miscellaneous Constants.
const (
	ownedNamespaceNumber = 1 // Number of namespaces owned, adjust to 4 for RHOAI deployment

	dsciInstanceName = "default-dsci" // Instance name for the DSCInitialization
	dscInstanceName  = "default-dsc"  // Instance name for the DataScienceCluster

	// Standard error messages format.
	resourceNotNilErrorMsg       = "Expected a non-nil resource object but got nil."
	resourceNotFoundErrorMsg     = "Expected resource '%s' of kind '%s' to exist, but it was not found or could not be retrieved."
	resourceFoundErrorMsg        = "Expected resource '%s' of kind '%s' to not exist, but it was found."
	resourceEmptyErrorMsg        = "Expected resource list '%s' of kind '%s' to contain resources, but it was empty."
	resourceListNotEmptyErrorMsg = "Expected resource list '%s' of kind '%s' to be empty, but it contains resources."
	resourceFetchErrorMsg        = "Error occurred while fetching the resource '%s' of kind '%s': %v"
	unexpectedErrorMismatchMsg   = "Expected error '%v' to match the actual error '%v' for resource of kind '%s'."
)

type Operator struct {
	nn                  types.NamespacedName
	skipOperatorGroup   bool
	globalOperatorGroup bool
	channel             string
}

// TestCaseOpts defines a function type that can be used to modify how individual test cases are executed.
type TestCaseOpts func(t *testing.T)

// RunTestCases runs a series of test cases, optionally in parallel based on the provided options.
// If the circuit breaker has tripped, remaining test cases are skipped with a clear message.
//
// Results are NOT recorded to the circuit breaker here because some suites call
// t.Parallel() inside their test functions (not via WithParallel). In that case
// t.Run returns immediately with a false "pass" that would reset the failure
// counter. Recording happens at the mustRun level instead, which reliably waits
// for all subtests to complete.
//
// Parameters:
//   - t (*testing.T): The test context passed into the test function.
//   - testCases ([]TestCase): A slice of test cases to execute.
//   - opts (...TestCaseOpts): Optional configuration options, like enabling parallel execution.
func RunTestCases(t *testing.T, testCases []TestCase, opts ...TestCaseOpts) {
	t.Helper()

	for _, testCase := range testCases {
		if circuitBreaker.IsOpen() {
			t.Run(testCase.name, func(t *testing.T) {
				t.Helper()
				circuitBreaker.SkipIfOpen(t)
			})
			continue
		}

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
			Labels: map[string]string{
				"opendatahub.io/created-by-e2e-tests": "true",
			},
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

// CreateDSC creates a DataScienceCluster CR.
func CreateDSC(name string, workbenchesNamespace string) *dscv2.DataScienceCluster {
	return &dscv2.DataScienceCluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DataScienceCluster",
			APIVersion: dscv2.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"opendatahub.io/created-by-e2e-tests": "true",
			},
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
				SparkOperator: componentApi.DSCSparkOperator{
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
			Labels: map[string]string{
				"opendatahub.io/created-by-e2e-tests": "true",
			},
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
		Object: map[string]any{
			"apiVersion": apiVersion,
			"kind":       "HardwareProfile",
			"metadata": map[string]any{
				"name":      name,
				"namespace": namespace,
			},
			"spec": map[string]any{
				"identifiers": []map[string]any{
					{
						"displayName":  "GPU",
						"identifier":   "nvidia.com/gpu",
						"minCount":     minCount.IntVal,
						"maxCount":     maxCount.IntVal,
						"defaultCount": defaultCount.IntVal,
						"resourceType": "Accelerator",
					},
				},
				"scheduling": map[string]any{
					"type": "Node",
					"node": map[string]any{
						"nodeSelector": map[string]any{
							"kubernetes.io/arch":             "amd64",
							"node-role.kubernetes.io/worker": "",
						},
						"tolerations": []map[string]any{
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
		Object: map[string]any{
			"apiVersion": "operator.openshift.io/v1",
			"kind":       "JobSetOperator",
			"metadata": map[string]any{
				"name": "cluster",
			},
			"spec": map[string]any{
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

// ensureOperatorsAreInstalled ensures the specified operators are installed using parallel test cases.
func (tc *TestContext) ensureOperatorsAreInstalled(t *testing.T, operators []Operator) {
	t.Helper()
	// Create and run test cases in parallel.
	testCases := make([]TestCase, len(operators))
	for i, op := range operators {
		testCases[i] = TestCase{
			name: fmt.Sprintf("Ensure %s is installed", op.nn.Name),
			testFn: func(t *testing.T) {
				t.Helper()
				switch {
				case op.skipOperatorGroup:
					tc.EnsureOperatorInstalledWithChannel(op.nn, op.channel)
				case op.globalOperatorGroup:
					tc.EnsureOperatorInstalledWithGlobalOperatorGroupAndChannel(op.nn, op.channel)
				default:
					tc.EnsureOperatorInstalledWithLocalOperatorGroupAndChannel(op.nn, op.channel)
				}
			},
		}
	}

	RunTestCases(t, testCases, WithParallel())
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
		return gateway.DashboardRouteNameRHOAI
	case cluster.OpenDataHub:
		return gateway.DashboardRouteNameODH
	default:
		return gateway.DashboardRouteNameODH
	}
}

func BackupDSCIandDSC(t *testing.T) (string, string) { //nolint:thelper
	// Initialize the test context.
	tc, err := NewTestContext(t)
	require.NoError(t, err, "Failed to initialize test context")

	dsciBackupPath, dscBackupPath := BackupDSCI(t, tc), BackupDSC(t, tc)
	t.Logf("Backup completed for DSCI and DSC: %s %s\n", dsciBackupPath, dscBackupPath)
	return dsciBackupPath, dscBackupPath
}

func BackupDSCI(t *testing.T, tc *TestContext) string {
	t.Helper()
	return BackupResource(t, tc, gvk.DSCInitialization, tc.DSCInitializationNamespacedName, "dsci")
}

func BackupDSC(t *testing.T, tc *TestContext) string {
	t.Helper()
	return BackupResource(t, tc, gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName, "dsc")
}

func BackupResource(t *testing.T, tc *TestContext, gvk schema.GroupVersionKind, name types.NamespacedName, backupFilePrefix string) string { //nolint:thelper
	fetched := tc.FetchResource(WithMinimalObject(gvk, name))
	if fetched == nil {
		t.Logf("Warning: Backup of %s was configured but %s %s not found, skipping its backup", gvk.Kind, gvk.Kind, name)
		return ""
	}
	backupPath, err := backupResourceToTempFile(fetched, backupFilePrefix)
	require.NoError(t, err, "Failed to backup %s %s to a file", gvk.Kind, name)
	t.Logf("%s %s backed up to %s", gvk.Kind, name, backupPath)
	return backupPath
}

func backupResourceToTempFile(obj *unstructured.Unstructured, prefix string) (string, error) {
	stripped := resources.StripServerMetadata(obj)
	data, err := json.Marshal(stripped.Object)
	if err != nil {
		return "", fmt.Errorf("failed to marshal resource %s %s: %w", stripped.GetKind(), stripped.GetName(), err)
	}
	f, err := os.CreateTemp("", prefix+"-*.json")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file for %s: %w", prefix, err)
	}
	defer func(f *os.File) {
		err := f.Close()
		if err != nil {
			fmt.Printf("Failed to close temp file %s: %v\n", f.Name(), err)
		}
	}(f)
	if _, err := f.Write(data); err != nil {
		if errRemove := os.Remove(f.Name()); errRemove != nil {
			return "", fmt.Errorf("Failed to remove backup file %s: %w after failed to write backup: %w", f.Name(), errRemove, err)
		}
		return "", fmt.Errorf("failed to write backup %s: %w", f.Name(), err)
	}
	return f.Name(), nil
}

func RestoreDSCIandDSCFromBackup(t *testing.T, dsciBackupPath, dscBackupPath string) { //nolint:thelper
	var tc *TestContext
	if dsciBackupPath != "" || dscBackupPath != "" {
		var err error
		// Initialize the test context.
		tc, err = NewTestContext(t)
		require.NoError(t, err, "Failed to initialize test context")
	}
	if dsciBackupPath != "" {
		RestoreDSCIFromBackup(t, tc, dsciBackupPath)
	}
	if dscBackupPath != "" {
		RestoreDSCFromBackup(t, tc, dscBackupPath)
	}
}

func RestoreDSCIFromBackup(t *testing.T, tc *TestContext, dsciBackupPath string) { //nolint:thelper
	RestoreResourceFromBackup(t, tc, dsciBackupPath)
	t.Logf("Restore completed for DSCI: %s. Waiting for reconciliation\n", dsciBackupPath)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DSCInitialization, tc.DSCInitializationNamespacedName),
		WithCondition(jq.Match(`.status.phase == "%s"`, status.ConditionTypeReady)),
	)
	t.Logf("DSCI reconciled successfully")
}

func RestoreDSCFromBackup(t *testing.T, tc *TestContext, dscBackupPath string) { //nolint:thelper
	tc.DeleteResources( // Remove due to nested components propagation not working correctly (RHOAIENG-61169)
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithWaitForDeletion(true),
		WithIgnoreNotFound(true),
	)

	RestoreResourceFromBackup(t, tc, dscBackupPath)
	t.Logf("Restore completed for DSC: %s. Waiting for reconciliation\n", dscBackupPath)
	tc.EnsureResourceExists(
		WithMinimalObject(gvk.DataScienceCluster, tc.DataScienceClusterNamespacedName),
		WithCondition(jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue)),
	)
	t.Logf("DSC reconciled successfully")
}

func RestoreResourceFromBackup(t *testing.T, tc *TestContext, backupPath string) { //nolint:thelper
	backedUpObject, err := loadResourceFromTempFile(backupPath)
	require.NoError(t, err, "Failed to load backup from file: %s", backupPath)

	tc.EventuallyResourceCreatedOrUpdated(
		WithObjectToCreate(backedUpObject),
		WithMutateFunc(func(obj *unstructured.Unstructured) error {
			obj.SetAnnotations(backedUpObject.GetAnnotations())
			obj.SetLabels(backedUpObject.GetLabels())
			backedUpObjectSpec, ok, err := unstructured.NestedFieldNoCopy(backedUpObject.Object, "spec")
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("failed to get spec from %s %s", backedUpObject.GetKind(), backedUpObject.GetName())
			}
			return unstructured.SetNestedField(obj.Object, backedUpObjectSpec, "spec")
		}),
		WithCustomErrorMsg("Failed to restore %s %s from backup", backedUpObject.GetKind(), backedUpObject.GetName()),
		WithEventuallyTimeout(30*time.Second),
		WithEventuallyPollingInterval(tc.TestTimeouts.defaultEventuallyPollInterval),
	)
	t.Logf("%s restored from %s", backedUpObject.GetKind(), backupPath)

	if err := os.Remove(backupPath); err != nil {
		t.Logf("Failed to remove %s backup file %s: %v", backedUpObject.GetKind(), backupPath, err)
	} else {
		t.Logf("%s backup file %s removed successfully", backedUpObject.GetKind(), backupPath)
	}
}

func loadResourceFromTempFile(path string) (*unstructured.Unstructured, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read backup file %s: %w", path, err)
	}
	obj := make(map[string]interface{})
	if err := json.Unmarshal(data, &obj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal backup from file %s: %w", path, err)
	}
	return &unstructured.Unstructured{Object: obj}, nil
}
