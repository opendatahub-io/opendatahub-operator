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
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	modelregistryctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
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
	telemetryOpName             = "opentelemetry-product"                    // Name of the Telemetry Operator
	openshiftOperatorsNamespace = "openshift-operators"                      // Namespace for OpenShift Operators
	telemetryOpNamespace        = "openshift-opentelemetry-operator"         // Namespace for the Telemetry Operator
	observabilityOpName         = "cluster-observability-operator"           // Name of the Cluster Observability Operator
	observabilityOpNamespace    = "openshift-cluster-observability-operator" // Namespace for the Cluster Observability Operator
	tempoOpName                 = "tempo-product"                            // Name of the Tempo Operator
	tempoOpNamespace            = "openshift-tempo-operator"                 // Namespace for the Tempo Operator
	controllerDeploymentODH     = "opendatahub-operator-controller-manager"  // Name of the ODH deployment
	controllerDeploymentRhoai   = "rhods-operator"                           // Name of the Rhoai deployment
	leaderWorkerSetOpName       = "leader-worker-set"                        // Name of the Leader Worker Set Operator
	leaderWorkerSetNamespace    = "openshift-lws-operator"                   // Namespace for the Leader Worker Set Operator
	leaderWorkerSetChannel      = "stable-v1.0"                              // Channel for the Leader Worker Set Operator
	kuadrantOperator            = "rhcl-operator"                            // Name of the Red Hat Connectivity Link Operator subscription.
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
func CreateDSCI(name, groupVersion string, appNamespace, monitoringNamespace string) *dsciv2.DSCInitialization {
	return &dsciv2.DSCInitialization{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DSCInitialization",
			APIVersion: groupVersion,
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
