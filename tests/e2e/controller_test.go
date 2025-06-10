package e2e_test

import (
	"flag"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/onsi/gomega/format"
	operatorv1 "github.com/openshift/api/operator/v1"
	routev1 "github.com/openshift/api/route/v1"
	ofapiv1 "github.com/operator-framework/api/pkg/operators/v1"
	ofapi "github.com/operator-framework/api/pkg/operators/v1alpha1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
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
type DeletionPolicy int

// Constants for the valid deletion policies.
const (
	DeletionPolicyAlways DeletionPolicy = iota
	DeletionPolicyOnFailure
	DeletionPolicyNever
)

var deletionPolicyName = map[DeletionPolicy]string{
	DeletionPolicyAlways:    "always",
	DeletionPolicyOnFailure: "on-failure",
	DeletionPolicyNever:     "never",
}

var stringToDeletionPolicy = map[string]DeletionPolicy{
	"always":     DeletionPolicyAlways,
	"on-failure": DeletionPolicyOnFailure,
	"never":      DeletionPolicyNever,
}

func (dp DeletionPolicy) String() string {
	return deletionPolicyName[dp]
}

func ParseDeletionPolicy(dp string) (DeletionPolicy, error) {
	value, found := stringToDeletionPolicy[dp]
	if !found {
		return 0, fmt.Errorf("invalid deletion policy: %s, accepted values are: %v", dp, maps.Keys(stringToDeletionPolicy))
	}
	return value, nil
}

// Struct to store test configurations.
type TestContextConfig struct {
	operatorNamespace string
	appsNamespace     string
	deletionPolicy    DeletionPolicy

	operatorControllerTest bool
	webhookTest            bool
	TestTimeouts           TestTimeouts
}

// TestGroup defines the test groups.
type TestGroup struct {
	name      string
	enabled   bool
	scenarios map[string]TestFn
	flags     arrayFlags
}

type TestTimeouts struct {
	defaultEventuallyTimeout        time.Duration
	mediumEventuallyTimeout         time.Duration
	longEventuallyTimeout           time.Duration
	defaultEventuallyPollInterval   time.Duration
	defaultConsistentlyTimeout      time.Duration
	defaultConsistentlyPollInterval time.Duration
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
			componentApi.LlamaStackOperatorComponentName:   llamastackOperatorTestSuite,
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
	return slices.AppendSeq(make([]string, 0, len(tg.scenarios)), maps.Keys(tg.scenarios))
}

func (tg *TestGroup) Validate() error {
	if tg.enabled == false {
		fmt.Printf("Test group %s is disabled, all the others group's configurations will be ignored.\n", tg.name)
		return nil
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
		enabledTests = slices.AppendSeq(make([]string, 0, len(tg.scenarios)), maps.Keys(tg.scenarios))
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
	// Gomega output config:
	format.MaxLength = 0 // 0 disables truncation entirely

	// Viper settings
	viper.SetEnvPrefix("E2E_TEST")
	replacer := strings.NewReplacer("-", "_")
	viper.SetEnvKeyReplacer(replacer)
	viper.AutomaticEnv()

	// Defaults
	// Gomega default values for Eventually/Consistently can be found here:
	// https://onsi.github.io/gomega/#making-asynchronous-assertions
	viper.SetDefault("defaultEventuallyTimeout", "5m")        // Timeout used for Eventually; overrides Gomega's default of 1 second.
	viper.SetDefault("mediumEventuallyTimeout", "7m")         // Medium timeout: for readiness checks (e.g., ClusterServiceVersion, DataScienceCluster).
	viper.SetDefault("longEventuallyTimeout", "10m")          // Long timeout: for more complex readiness (e.g., DSCInitialization, KServe).
	viper.SetDefault("defaultEventuallyPollInterval", "2s")   // Polling interval for Eventually; overrides Gomega's default of 10 milliseconds.
	viper.SetDefault("defaultConsistentlyTimeout", "10s")     // Duration used for Consistently; overrides Gomega's default of 2 seconds.
	viper.SetDefault("defaultConsistentlyPollInterval", "2s") // Polling interval for Consistently; overrides Gomega's default of 50 milliseconds.

	// Flags
	pflag.String("operator-namespace", "redhat-ods-operator", "Namespace where the odh operator is deployed")
	checkEnvVarBindingError(viper.BindEnv("operator-namespace", viper.GetEnvPrefix()+"_OPERATOR_NAMESPACE"))
	pflag.String("applications-namespace", "redhat-ods-applications", "Namespace where the odh applications are deployed")
	checkEnvVarBindingError(viper.BindEnv("applications-namespace", viper.GetEnvPrefix()+"_APPLICATIONS_NAMESPACE"))
	pflag.String("deletion-policy", "always",
		"Specify when to delete DataScienceCluster, DSCInitialization, and controllers. Options: always, on-failure, never.")
	checkEnvVarBindingError(viper.BindEnv("deletion-policy", viper.GetEnvPrefix()+"_DELETION_POLICY"))

	pflag.Bool("test-operator-controller", true, "run operator controller tests")
	checkEnvVarBindingError(viper.BindEnv("test-operator-controller", viper.GetEnvPrefix()+"_OPERATOR_CONTROLLER"))
	pflag.Bool("test-webhook", true, "run webhook tests")
	checkEnvVarBindingError(viper.BindEnv("test-webhook", viper.GetEnvPrefix()+"_WEBHOOK"))

	// Component flags
	componentNames := strings.Join(Components.Names(), ", ")
	pflag.Bool("test-components", Components.enabled, "Enable testing of individual components specified by --test-component flag")
	checkEnvVarBindingError(viper.BindEnv("test-components", viper.GetEnvPrefix()+"_COMPONENTS"))
	pflag.StringSlice("test-component", Components.Names(), "Run tests for the specified component. Valid names: "+componentNames)
	checkEnvVarBindingError(viper.BindEnv("test-component", viper.GetEnvPrefix()+"_COMPONENT"))

	// Service flags
	serviceNames := strings.Join(Services.Names(), ", ")
	pflag.Bool("test-services", Services.enabled, "Enable testing of individual services specified by --test-service flag")
	checkEnvVarBindingError(viper.BindEnv("test-services", viper.GetEnvPrefix()+"_SERVICES"))
	pflag.StringSlice("test-service", Services.Names(), "Run tests for the specified service. Valid names: "+serviceNames)
	checkEnvVarBindingError(viper.BindEnv("test-service", viper.GetEnvPrefix()+"_SERVICE"))

	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)

	// workaround for pflag limitation see: https://github.com/spf13/pflag/issues/238
	if err := ParseTestFlags(); err != nil {
		fmt.Printf("Failed to parse go test flags (i.e. the one with -test. prefix): %v", err)
		os.Exit(1)
	}

	pflag.Parse()
	if err := viper.BindPFlags(pflag.CommandLine); err != nil {
		fmt.Printf("Error in binding tests flags: %s", err.Error())
		os.Exit(1)
	}

	testOpts.TestTimeouts = TestTimeouts{
		defaultEventuallyTimeout:        viper.GetDuration("defaultEventuallyTimeout"),
		mediumEventuallyTimeout:         viper.GetDuration("mediumEventuallyTimeout"),
		longEventuallyTimeout:           viper.GetDuration("longEventuallyTimeout"),
		defaultEventuallyPollInterval:   viper.GetDuration("defaultEventuallyPollInterval"),
		defaultConsistentlyTimeout:      viper.GetDuration("defaultConsistentlyTimeout"),
		defaultConsistentlyPollInterval: viper.GetDuration("defaultConsistentlyPollInterval"),
	}
	testOpts.operatorNamespace = viper.GetString("operator-namespace")
	testOpts.appsNamespace = viper.GetString("applications-namespace")
	var err error
	if testOpts.deletionPolicy, err = ParseDeletionPolicy(viper.GetString("deletion-policy")); err != nil {
		fmt.Print(err.Error())
		os.Exit(1)
	}
	testOpts.operatorControllerTest = viper.GetBool("test-operator-controller")
	testOpts.webhookTest = viper.GetBool("test-webhook")
	Components.enabled = viper.GetBool("test-components")
	Components.flags = viper.GetStringSlice("test-component")
	Services.enabled = viper.GetBool("test-services")
	Services.flags = viper.GetStringSlice("test-service")

	// Config validation
	if err := Components.Validate(); err != nil {
		fmt.Printf("test-component: %s", err.Error())
		os.Exit(1)
	}

	if err := Services.Validate(); err != nil {
		fmt.Printf("test-service: %s", err.Error())
		os.Exit(1)
	}

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

func checkEnvVarBindingError(err error) {
	if err != nil {
		fmt.Printf("Error in binding tests env var: %s", err.Error())
		os.Exit(1)
	}
}
