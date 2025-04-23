package e2e_test

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/onsi/gomega/format"
	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	ofapiv1 "github.com/operator-framework/api/pkg/operators/v1"
	ofapi "github.com/operator-framework/api/pkg/operators/v1alpha1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"golang.org/x/exp/maps"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/api/features/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
)

// Test function signature.
type TestFn func(t *testing.T)

// DeletionPolicy type representing the deletion policy.
type DeletionPolicy string

// Constants for the valid deletion policies.
const (
	DeletionPolicyAlways    DeletionPolicy = "always"
	DeletionPolicyOnFailure DeletionPolicy = "on-failure"
	DeletionPolicyNever     DeletionPolicy = "never"
)

// Struct to store test configurations.
type TestContextConfig struct {
	operatorNamespace string
	appsNamespace     string
	deletionPolicy    DeletionPolicy

	operatorControllerTest bool
	webhookTest            bool
}

// TestGroup defines the test groups.
type TestGroup struct {
	name      string
	enabled   bool
	scenarios map[string]TestFn
	flags     arrayFlags
}

type TestCase struct {
	name   string
	testFn func(t *testing.T)
}

var (
	Scheme   = runtime.NewScheme()
	testOpts = TestContextConfig{}

	Components = TestGroup{
		name:    "components",
		enabled: true,
		scenarios: map[string]TestFn{
			componentApi.DashboardComponentName:            dashboardTestSuite,
			componentApi.RayComponentName:                  rayTestSuite,
			componentApi.ModelRegistryComponentName:        modelRegistryTestSuite,
			componentApi.TrustyAIComponentName:             trustyAITestSuite,
			componentApi.KueueComponentName:                kueueTestSuite,
			componentApi.TrainingOperatorComponentName:     trainingOperatorTestSuite,
			componentApi.DataSciencePipelinesComponentName: dataSciencePipelinesTestSuite,
			componentApi.CodeFlareComponentName:            codeflareTestSuite,
			componentApi.WorkbenchesComponentName:          workbenchesTestSuite,
			componentApi.KserveComponentName:               kserveTestSuite,
			componentApi.ModelMeshServingComponentName:     modelMeshServingTestSuite,
			componentApi.ModelControllerComponentName:      modelControllerTestSuite,
			componentApi.FeastOperatorComponentName:        feastOperatorTestSuite,
		},
	}

	Services = TestGroup{
		name:    "services",
		enabled: true,
		scenarios: map[string]TestFn{
			serviceApi.MonitoringServiceName: monitoringTestSuite,
			serviceApi.AuthServiceName:       authControllerTestSuite,
		},
	}
)

// Custom flag handling.
type arrayFlags []string

// String returns the string representation of the arrayFlags.
func (i *arrayFlags) String() string {
	return fmt.Sprintf("%v", *i)
}

// Set appends a new value to the arrayFlags.
func (i *arrayFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}

func (tg *TestGroup) String() string {
	return tg.name
}

func (tg *TestGroup) Names() []string {
	return maps.Keys(tg.scenarios)
}

func (tg *TestGroup) Validate() error {
	if tg.enabled == false && len(tg.flags) != 0 {
		return errors.New("enabling individual scenarios is not supported when the entire group is disabled")
	}

	for _, name := range tg.flags {
		name = strings.TrimLeft(name, "!")
		if _, ok := tg.scenarios[name]; !ok {
			validValues := strings.Join(tg.Names(), ", ")
			return fmt.Errorf("unsupported value %s, valid values are: %s", name, validValues)
		}
	}

	return nil
}

func (tg *TestGroup) Run(t *testing.T) {
	t.Helper()

	if !tg.enabled {
		t.Skipf("Test group %s is disabled", tg.name)
		return
	}

	disabledTests := make([]string, 0)
	enabledTests := make([]string, 0)

	for _, name := range tg.flags {
		if strings.HasPrefix(name, "!") {
			disabledTests = append(disabledTests, strings.TrimLeft(name, "!"))
		} else {
			enabledTests = append(enabledTests, name)
		}
	}

	// Run all tests if none are explicitly enabled
	if len(enabledTests) == 0 {
		enabledTests = maps.Keys(tg.scenarios)
	}

	// Remove disabled tests
	enabledTests = slices.DeleteFunc(enabledTests, func(n string) bool {
		return slices.Contains(disabledTests, n)
	})

	// Run each test case
	for testName, testFunc := range tg.scenarios {
		if !slices.Contains(enabledTests, testName) {
			t.Logf("Skipping tests for %s/%s", tg.name, testName)
			continue
		}

		mustRun(t, testName, testFunc)
	}
}

// Helper function to handle cleanup logic.
func handleCleanup(t *testing.T) {
	t.Helper()

	// Cleanup logic to be shared for "always" and "on-failure" policies
	t.Cleanup(func() {
		if t.Failed() {
			fmt.Println("Test failed, running cleanup...")
			CleanupAllResources(t)
		}
	})
}

// TestOdhOperator sets up the testing suite for ODH Operator.
func TestOdhOperator(t *testing.T) {
	registerSchemes()

	log.SetLogger(zap.New(zap.UseDevMode(true)))

	// Removing any existing default DSC, DSCI, and Auth CRs before starting the tests.
	CleanupDefaultResources(t)

	if testOpts.operatorControllerTest {
		// individual test suites after the operator is running
		mustRun(t, "ODH Manager E2E Tests", odhOperatorTestSuite)
	}

	// Run DSCI/DSC test suites
	mustRun(t, "DSCInitialization and DataScienceCluster management E2E Tests", dscManagementTestSuite)

	// Run components and services test suites
	mustRun(t, Components.String(), Components.Run)
	mustRun(t, Services.String(), Services.Run)

	// Deletion logic based on deletionPolicy
	switch testOpts.deletionPolicy {
	case DeletionPolicyAlways:
		// Always run deletion tests
		fmt.Println("Deletion Policy: Always. Running deletion tests.")
		if testOpts.operatorControllerTest {
			// this is a negative test case, since by using the positive CM('true'), even CSV gets deleted which leaves no operator pod in prow
			mustRun(t, "Deletion ConfigMap E2E Tests", cfgMapDeletionTestSuite)
		}
		mustRun(t, "DataScienceCluster/DSCInitialization Deletion E2E Tests", deletionTestSuite)

		// Always perform cleanup after failure
		handleCleanup(t)

	case DeletionPolicyOnFailure:
		// Only cleanup if the test fails
		fmt.Println("Deletion Policy: On Failure. Will delete resources only on failure.")
		handleCleanup(t)

	case DeletionPolicyNever:
		// Do nothing for "never" policy
		fmt.Println("Deletion Policy: Never. Skipping deletion tests.")

	default:
		fmt.Printf("Unknown deletion-policy: %s", testOpts.deletionPolicy)
		os.Exit(1)
	}
}

func TestMain(m *testing.M) {
	// call flag.Parse() here if TestMain uses flags
	flag.StringVar(&testOpts.operatorNamespace, "operator-namespace", "redhat-ods-operator", "Namespace where the odh operator is deployed")
	flag.StringVar(&testOpts.appsNamespace, "applications-namespace", "redhat-ods-applications", "Namespace where the odh applications are deployed")
	flag.StringVar((*string)(&testOpts.deletionPolicy), "deletion-policy", "always",
		"Specify when to delete DataScienceCluster, DSCInitialization, and controllers. Options: always, on-failure, never.")

	flag.BoolVar(&testOpts.operatorControllerTest, "test-operator-controller", true, "run operator controller tests")
	flag.BoolVar(&testOpts.webhookTest, "test-webhook", true, "run webhook tests")

	// Component flags
	componentNames := strings.Join(Components.Names(), ", ")
	flag.BoolVar(&Components.enabled, "test-components", Components.enabled, "Enable tests for components")
	flag.Var(&Components.flags, "test-component", "Run tests for the specified component. Valid names: "+componentNames)

	// Service flags
	serviceNames := strings.Join(Services.Names(), ", ")
	flag.BoolVar(&Services.enabled, "test-services", Services.enabled, "Enable tests for services")
	flag.Var(&Services.flags, "test-service", "Run tests for the specified service. Valid names: "+serviceNames)

	flag.Parse()

	if err := Components.Validate(); err != nil {
		fmt.Printf("test-component: %s", err.Error())
		os.Exit(1)
	}

	if err := Services.Validate(); err != nil {
		fmt.Printf("test-service: %s", err.Error())
		os.Exit(1)
	}

	// Gomega output config:
	format.MaxLength = 0 // 0 disables truncation entirely

	os.Exit(m.Run())
}

// registerSchemes registers all necessary schemes for testing.
func registerSchemes() {
	schemes := []func(*runtime.Scheme) error{
		clientgoscheme.AddToScheme,
		routev1.AddToScheme,
		apiextv1.AddToScheme,
		autoscalingv1.AddToScheme,
		dsciv1.AddToScheme,
		dscv1.AddToScheme,
		featurev1.AddToScheme,
		monitoringv1.AddToScheme,
		ofapi.AddToScheme,
		operatorv1.AddToScheme,
		componentApi.AddToScheme,
		serviceApi.AddToScheme,
		ofapiv1.AddToScheme,
	}

	for _, schemeFn := range schemes {
		utilruntime.Must(schemeFn(Scheme))
	}
}

// mustRun executes a test and stops execution if it fails.
func mustRun(t *testing.T, name string, testFunc func(t *testing.T)) {
	t.Helper()

	// If the test already failed, skip running the next test
	if t.Failed() {
		return
	}

	if !t.Run(name, testFunc) {
		t.Logf("Stopping: %s test failed.", name)
		t.Fail()
	}
}
