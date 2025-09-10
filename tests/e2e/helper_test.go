package e2e_test

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"

	gTypes "github.com/onsi/gomega/types"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	modelregistryctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/components/modelregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

// Namespace and Operator Constants.
const (
	// Namespaces for various components.
	knativeServingNamespace = "knative-serving" // Namespace for Knative Serving components

	// Operators constants.
	defaultOperatorChannel       = "stable"                           // The default channel to install/check operators
	serviceMeshOpName            = "servicemeshoperator"              // Name of the Service Mesh Operator
	serverlessOpName             = "serverless-operator"              // Name of the Serverless Operator
	authorinoOpName              = "authorino-operator"               // Name of the Serverless Operator
	kueueOpName                  = "kueue-operator"                   // Name of the Kueue Operator
	telemetryOpName              = "opentelemetry-product"            // Name of the Telemetry Operator
	telemetryOpNamespace         = "openshift-opentelemetry-operator" // Namespace for the Telemetry Operator
	serviceMeshControlPlane      = "data-science-smcp"                // Service Mesh control plane name
	serviceMeshNamespace         = "istio-system"                     // Namespace for Istio Service Mesh control plane
	serviceMeshMetricsCollection = "Istio"                            // Metrics collection for Service Mesh (e.g., Istio)
	serviceMeshMemberName        = "default"
	observabilityOpName          = "cluster-observability-operator"           // Name of the Cluster Observability Operator
	observabilityOpNamespace     = "openshift-cluster-observability-operator" // Namespace for the Cluster Observability Operator
	tempoOpName                  = "tempo-product"                            // Name of the Tempo Operator
	tempoOpNamespace             = "openshift-tempo-operator"                 // Namespace for the Tempo Operator
	opentelemetryOpName          = "opentelemetry-product"                    // Name of the OpenTelemetry Operator
	opentelemetryOpNamespace     = "openshift-opentelemetry-operator"         // Namespace for the OpenTelemetry Operator
	controllerDeploymentODH      = "opendatahub-operator-controller-manager"  // Name of the ODH deployment
	controllerDeploymentRhoai    = "rhods-operator"                           // Name of the Rhoai deployment
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
func CreateDSCI(name, appNamespace, monitoringNamespace string) *dsciv1.DSCInitialization {
	return &dsciv1.DSCInitialization{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DSCInitialization",
			APIVersion: dsciv1.GroupVersion.String(),
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
			ServiceMesh: &infrav1.ServiceMeshSpec{
				ManagementState: operatorv1.Managed,
				ControlPlane: infrav1.ControlPlaneSpec{
					Name:              serviceMeshControlPlane,
					Namespace:         serviceMeshNamespace,
					MetricsCollection: serviceMeshMetricsCollection,
				},
			},
		},
	}
}

// CreateDSC creates a DataScienceCluster CR.
func CreateDSC(name string) *dscv1.DataScienceCluster {
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
					KserveCommonSpec: componentApi.KserveCommonSpec{
						DefaultDeploymentMode: componentApi.Serverless,
						Serving: infrav1.ServingSpec{
							ManagementState: operatorv1.Managed,
							Name:            knativeServingNamespace,
							IngressGateway: infrav1.GatewaySpec{
								Certificate: infrav1.CertificateSpec{
									Type: infrav1.OpenshiftDefaultIngress,
								},
							},
						},
					},
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

// getOperatorSelector returns selector based on platform.
func (tc *TestContext) getOperatorPodSelector() labels.Selector {
	platform := tc.FetchPlatformRelease()
	switch platform {
	case cluster.SelfManagedRhoai, cluster.ManagedRhoai:
		return labels.SelectorFromSet(labels.Set{"name": "rhods-operator"})
	case cluster.OpenDataHub:
		return labels.SelectorFromSet(labels.Set{"control-plane": "controller-manager"})
	default:
		return labels.SelectorFromSet(labels.Set{"control-plane": "controller-manager"})
	}
}

// getControllerDeploymentName returns deployment name based on platform.
func (tc *TestContext) getControllerDeploymentName() string {
	platform := tc.FetchPlatformRelease()
	switch platform {
	case cluster.SelfManagedRhoai, cluster.ManagedRhoai:
		return controllerDeploymentRhoai
	case cluster.OpenDataHub:
		return controllerDeploymentODH
	default:
		return controllerDeploymentODH
	}
}
