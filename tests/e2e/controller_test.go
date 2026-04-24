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
	userv1 "github.com/openshift/api/user/v1"
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
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/api/features/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
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
	tag                  string
	operatorNamespace    string
	appsNamespace        string
	workbenchesNamespace string
	monitoringNamespace  string
	deletionPolicy       DeletionPolicy

	cleanUpPreviousResources         bool
	dependantOperatorsManagementTest bool
	dscManagementTest                bool
	dscValidationTest                bool
	operatorControllerTest           bool
	operatorResilienceTest           bool
	webhookTest                      bool
	v2tov3upgradeTest                bool
	circuitBreakerEnabled            bool
	circuitBreakerThreshold          int
	TestTimeouts                     TestTimeouts
}

// TestGroup defines the test groups.
type TestGroup struct {
	name      string
	enabled   bool
	parallel  bool
	scenarios []map[string]TestFn
	flags     arrayFlags
}

type TestTimeouts struct {
	defaultEventuallyTimeout        time.Duration
	shortEventuallyTimeout          time.Duration
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
		name:     "components",
		enabled:  true,
		parallel: true,
		scenarios: []map[string]TestFn{
			{
				componentApi.DashboardComponentName:            dashboardTestSuite,
				componentApi.RayComponentName:                  rayTestSuite,
				componentApi.ModelRegistryComponentName:        modelRegistryTestSuite,
				componentApi.TrainingOperatorComponentName:     trainingOperatorTestSuite,
				componentApi.TrainerComponentName:              trainerTestSuite,
				componentApi.DataSciencePipelinesComponentName: dataSciencePipelinesTestSuite,
				componentApi.WorkbenchesComponentName:          workbenchesTestSuite,
				componentApi.KserveComponentName:               kserveTestSuite,
				componentApi.FeastOperatorComponentName:        feastOperatorTestSuite,
				componentApi.LlamaStackOperatorComponentName:   llamastackOperatorTestSuite,
				componentApi.SparkOperatorComponentName:        sparkOperatorTestSuite,
			},
			{
				// Kueue tests depends on Workbenches, so must not run with Workbenches tests in parallel
				componentApi.KueueComponentName: kueueTestSuite,
				// ModelController tests depends on KServe and ModelRegistry, so must not run with KServe, ModelRegistry, TrustyAI or ModelsAsService tests in parallel
				componentApi.ModelControllerComponentName: modelControllerTestSuite,
			},
			{
				// TrustyAI tests depends on KServe, so must not run with Kserve, ModelController or ModelsAsService tests in parallel
				componentApi.TrustyAIComponentName: trustyAITestSuite,
				// MLflowOperator tests should not run in parallel with Workbenches tests, as Workbenches tests integration with MLflowOperator
				componentApi.MLflowOperatorComponentName: mlflowOperatorTestSuite,
			},
			{
				// ModelsAsService tests depends on KServe, so must not run with Kserve, ModelController or TrustyAI tests in parallel
				componentApi.ModelsAsServiceComponentName: modelsAsServiceTestSuite,
			},
			{
				// run external operator degraded monitoring tests isolated from other component tests
				componentApi.KserveComponentName:  kserveDegradedMonitoringTestSuite,
				componentApi.KueueComponentName:   kueueDegradedMonitoringTestSuite,
				componentApi.TrainerComponentName: trainerDegradedMonitoringTestSuite,
			},
		},
	}

	Services = TestGroup{
		name:     "services",
		enabled:  true,
		parallel: true,
		scenarios: []map[string]TestFn{{
			serviceApi.MonitoringServiceName: monitoringTestSuite,
			serviceApi.AuthServiceName:       authControllerTestSuite,
			serviceApi.GatewayServiceName:    gatewayTestSuite,
		}},
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
	names := make([]string, 0, len(tg.scenarios))
	for _, group := range tg.scenarios {
		names = slices.AppendSeq(names, maps.Keys(group))
	}
	return names
}

func (tg *TestGroup) Validate() error {
	if tg.enabled == false {
		fmt.Printf("Test group %s is disabled, all the others group's configurations will be ignored.\n", tg.name)
		return nil
	}

	testNameMap := map[string]bool{}
	for _, name := range tg.Names() {
		testNameMap[name] = true
	}

	for _, name := range tg.flags {
		name = strings.TrimLeft(name, "!")
		if _, ok := testNameMap[name]; !ok {
			validValues := strings.Join(tg.Names(), ", ")
			return fmt.Errorf("unsupported value %s, valid values are: %s", name, validValues)
		}
	}

	return nil
}

// resolveEnabledTests parses tg.flags into a set of test names that should
// run. Flags without "!" are inclusions, flags prefixed with "!" are exclusions.
func (tg *TestGroup) resolveEnabledTests() map[string]bool {
	enabledTests := make(map[string]bool)
	disabledTests := make([]string, 0)

	for _, name := range tg.flags {
		if strings.HasPrefix(name, "!") {
			disabledTests = append(disabledTests, strings.TrimLeft(name, "!"))
		} else {
			enabledTests[name] = true
		}
	}

	if len(enabledTests) == 0 {
		for _, name := range tg.Names() {
			enabledTests[name] = true
		}
	}

	for _, name := range disabledTests {
		delete(enabledTests, name)
	}

	return enabledTests
}

func (tg *TestGroup) Run(t *testing.T) {
	t.Helper()

	if !tg.enabled {
		t.Skipf("Test group %s is disabled", tg.name)
		return
	}

	enabledTests := tg.resolveEnabledTests()

	for i, group := range tg.scenarios {
		mustRun(t, fmt.Sprintf("group %d", i+1), func(t *testing.T) {
			t.Helper()

			groupNames := slices.AppendSeq(make([]string, 0, len(group)), maps.Keys(group))
			slices.Sort(groupNames)

			for _, testName := range groupNames {
				testFunc := group[testName]
				if _, ok := enabledTests[testName]; !ok {
					t.Logf("Skipping tests for %s/%s", tg.name, testName)
					continue
				}
				if tg.parallel {
					mustRun(t, testName, testFunc, WithParallel())
				} else {
					mustRun(t, testName, testFunc)
				}
			}
		})
	}
}

// RunSingle returns a test function that runs only the named test from this group.
func (tg *TestGroup) RunSingle(name string) func(t *testing.T) {
	return func(t *testing.T) {
		t.Helper()

		if !tg.enabled {
			t.Skipf("Test group %s is disabled", tg.name)
			return
		}

		if !tg.isTestEnabled(name) {
			t.Skipf("Test %s is disabled by flags in group %s", name, tg.name)
			return
		}

		for _, group := range tg.scenarios {
			if testFunc, ok := group[name]; ok {
				testFunc(t)
				return
			}
		}

		t.Skipf("Test %s not found in group %s", name, tg.name)
	}
}

// isTestEnabled returns true if the named test should run given the group's flags.
func (tg *TestGroup) isTestEnabled(name string) bool {
	_, ok := tg.resolveEnabledTests()[name]
	return ok
}

// RunExcluding returns a test function that runs this group but skips the
// named test. Used together with RunSingle to split a test out of its group.
func (tg *TestGroup) RunExcluding(excludeName string) func(t *testing.T) {
	return func(t *testing.T) {
		t.Helper()

		if !tg.enabled {
			t.Skipf("Test group %s is disabled", tg.name)
			return
		}

		enabledTests := tg.resolveEnabledTests()
		delete(enabledTests, excludeName)

		if len(enabledTests) == 0 {
			t.Skipf("No tests remaining in group %s after excluding %s", tg.name, excludeName)
			return
		}

		for i, group := range tg.scenarios {
			mustRun(t, fmt.Sprintf("group %d", i+1), func(t *testing.T) {
				t.Helper()

				groupNames := slices.AppendSeq(make([]string, 0, len(group)), maps.Keys(group))
				slices.Sort(groupNames)

				for _, testName := range groupNames {
					testFunc := group[testName]
					if _, ok := enabledTests[testName]; !ok {
						t.Logf("Skipping tests for %s/%s", tg.name, testName)
						continue
					}
					if tg.parallel {
						mustRun(t, testName, testFunc, WithParallel())
					} else {
						mustRun(t, testName, testFunc)
					}
				}
			})
		}
	}
}

// Helper function to handle cleanup logic.
func handleCleanup(t *testing.T) {
	t.Helper()

	// Cleanup logic to be shared for "always" and "on-failure" policies
	t.Cleanup(func() {
		if t.Failed() {
			fmt.Println("Test failed, running cleanup...")
			CleanupPreviousTestResources(t)
		}
	})
}

// testDeadlineMargin is the safety margin subtracted from the go test -timeout deadline.
const testDeadlineMargin = 5 * time.Minute

// TestOdhOperator sets up the testing suite for the Operator.
func TestOdhOperator(t *testing.T) {
	// Set up global panic handler for comprehensive debugging
	defer HandleGlobalPanic()

	registerSchemes()

	log.SetLogger(zap.New(zap.UseDevMode(true)))

	if deadline, ok := t.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > testDeadlineMargin {
			done := make(chan struct{})
			watchdog := time.AfterFunc(remaining-testDeadlineMargin, func() {
				select {
				case <-done:
					return
				default:
					t.Errorf("Internal timeout watchdog fired (%s before go test deadline) — marking test failed to trigger cleanup and preserve JUnit output", testDeadlineMargin)
				}
			})
			t.Cleanup(func() {
				close(done)
				watchdog.Stop()
			})
		}
	}

	if testOpts.circuitBreakerEnabled {
		healthChecker := NewClusterHealthChecker()
		circuitBreaker = NewCircuitBreaker(
			testOpts.circuitBreakerThreshold,
			healthChecker,
		)
		t.Cleanup(func() {
			circuitBreaker.LogSummary()
			if circuitBreaker.TotalTrips() > 0 {
				t.Fatalf("Circuit breaker tripped: %s", circuitBreaker.TripReason())
			}
		})

		preflight := healthChecker.Check()
		if !preflight.Healthy {
			circuitBreaker.ForceTrip(fmt.Sprintf(
				"Pre-flight health check failed: [%s]",
				strings.Join(preflight.Issues, "; ")))
			return
		}
	}

	// Remove any leftover resources from previous test runs before starting if the cleanup flag is enabled
	if testOpts.cleanUpPreviousResources {
		CleanupPreviousTestResources(t)
	}

	if collector := startMetricsCollectorIfEnabled(); collector != nil {
		defer collector.Stop()
	}

	if testOpts.dependantOperatorsManagementTest {
		mustRun(t, "Dependant Operators Management E2E Tests", dependantOperatorsManagementTestSuite)
	}

	if testOpts.dscManagementTest {
		// Run DSCI/DSC management test suite
		mustRun(t, "DSCInitialization and DataScienceCluster management E2E Tests", dscManagementTestSuite)
	}

	if testOpts.operatorControllerTest {
		// individual test suites after the operator is running
		mustRun(t, "Operator Manager E2E Tests", odhOperatorTestSuite)
	}

	if testOpts.dscValidationTest {
		// Run DSCI/DSC test validation test suite
		mustRun(t, "DSCInitialization and DataScienceCluster validation E2E Tests", dscValidationTestSuite)
	}

	// Run monitoring before components — monitoring setup is a prerequisite.
	mustRun(t, serviceApi.MonitoringServiceName, Services.RunSingle(serviceApi.MonitoringServiceName))

	// Run components test suites
	mustRun(t, Components.String(), Components.Run)

	// Run remaining services (auth, gateway)
	mustRun(t, Services.String(), Services.RunExcluding(serviceApi.MonitoringServiceName))

	// Run operator resilience test suites after functional tests
	if testOpts.operatorResilienceTest {
		mustRun(t, "Operator Resilience E2E Tests", operatorResilienceTestSuite)
	}

	// Run V2 to V3 upgrade test suites
	if testOpts.v2tov3upgradeTest {
		mustRun(t, "V2 to V3 upgrade E2E Tests", v2Tov3UpgradeTestSuite)
	}

	// Run ConfigMap deletion test suite
	if testOpts.operatorControllerTest {
		// this is a negative test case, since by using the positive CM('true'), even CSV gets deleted which leaves no operator pod in prow
		mustRun(t, "Deletion ConfigMap E2E Tests", cfgMapDeletionTestSuite)
	}

	// Run V2 to V3 upgrade test suites that needs to delete DSC and DSCI at the last position
	if testOpts.v2tov3upgradeTest {
		mustRun(t, "upgrade DSC and DSCI v1 API", v2Tov3UpgradeDeletingDscDsciTestSuite)
	}

	// Deletion logic based on deletionPolicy
	switch testOpts.deletionPolicy {
	case DeletionPolicyAlways:
		// Always run deletion tests
		fmt.Println("Deletion Policy: Always. Running deletion tests.")
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
		t.Fatalf("Unknown deletion-policy: %s", testOpts.deletionPolicy)
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
	viper.SetDefault("shortEventuallyTimeout", "10s")         // Timeout used for Eventually; overrides Gomega's default of 1 second.
	viper.SetDefault("mediumEventuallyTimeout", "7m")         // Medium timeout: for readiness checks (e.g., ClusterServiceVersion, DataScienceCluster).
	viper.SetDefault("longEventuallyTimeout", "10m")          // Long timeout: for more complex readiness (e.g., DSCInitialization, KServe).
	viper.SetDefault("defaultEventuallyPollInterval", "10s")  // Polling interval for Eventually; overrides Gomega's default of 10 milliseconds.
	viper.SetDefault("defaultConsistentlyTimeout", "20s")     // Duration used for Consistently; overrides Gomega's default of 2 seconds.
	viper.SetDefault("defaultConsistentlyPollInterval", "5s") // Polling interval for Consistently; overrides Gomega's default of 50 milliseconds.

	// Flags
	pflag.String("operator-namespace", "opendatahub-operator-system", "Namespace where the odh operator is deployed")
	checkEnvVarBindingError(viper.BindEnv("operator-namespace", viper.GetEnvPrefix()+"_OPERATOR_NAMESPACE"))
	pflag.String("applications-namespace", "opendatahub", "Namespace where the odh applications are deployed")
	checkEnvVarBindingError(viper.BindEnv("applications-namespace", viper.GetEnvPrefix()+"_APPLICATIONS_NAMESPACE"))
	pflag.String("workbenches-namespace", "opendatahub", "Namespace where the workbenches are deployed")
	checkEnvVarBindingError(viper.BindEnv("workbenches-namespace", viper.GetEnvPrefix()+"_WORKBENCHES_NAMESPACE"))
	pflag.String("dsc-monitoring-namespace", "opendatahub", "Namespace where the odh monitoring is deployed")
	checkEnvVarBindingError(viper.BindEnv("dsc-monitoring-namespace", viper.GetEnvPrefix()+"_DSC_MONITORING_NAMESPACE"))
	pflag.String("deletion-policy", "always",
		"Specify when to delete DataScienceCluster, DSCInitialization, and controllers. Options: always, on-failure, never.")
	checkEnvVarBindingError(viper.BindEnv("deletion-policy", viper.GetEnvPrefix()+"_DELETION_POLICY"))

	tagNames := make([]string, 0, len(allowedTags))
	for _, tag := range allowedTags {
		tagNames = append(tagNames, string(tag))
	}
	pflag.String("tag", "All", "Tag to run tests for. Options: "+strings.Join(tagNames, ", "))
	checkEnvVarBindingError(viper.BindEnv("tag", viper.GetEnvPrefix()+"_TAG"))

	pflag.Bool("clean-up-previous-resources", true, "clean up previous resources before running tests")
	checkEnvVarBindingError(viper.BindEnv("clean-up-previous-resources", viper.GetEnvPrefix()+"_CLEAN_UP_PREVIOUS_RESOURCES"))
	pflag.Bool("test-operator-controller", true, "run operator controller tests")
	checkEnvVarBindingError(viper.BindEnv("test-operator-controller", viper.GetEnvPrefix()+"_OPERATOR_CONTROLLER"))
	pflag.Bool("test-dependant-operators-management", true, "run dependant operators management tests")
	checkEnvVarBindingError(viper.BindEnv("test-dependant-operators-management", viper.GetEnvPrefix()+"_DEPENDANT_OPERATORS_MANAGEMENT"))
	pflag.Bool("test-dsc-management", true, "run DSCI/DSC management tests")
	checkEnvVarBindingError(viper.BindEnv("test-dsc-management", viper.GetEnvPrefix()+"_DSC_MANAGEMENT"))
	pflag.Bool("test-dsc-validation", true, "run DSCI/DSC validation tests")
	checkEnvVarBindingError(viper.BindEnv("test-dsc-validation", viper.GetEnvPrefix()+"_DSC_VALIDATION"))
	pflag.Bool("test-operator-resilience", true, "run operator resilience tests")
	checkEnvVarBindingError(viper.BindEnv("test-operator-resilience", viper.GetEnvPrefix()+"_OPERATOR_RESILIENCE"))
	pflag.Bool("test-operator-v2tov3upgrade", true, "run V2 to V3 upgrade tests")
	checkEnvVarBindingError(viper.BindEnv("test-operator-v2tov3upgrade", viper.GetEnvPrefix()+"_OPERATOR_V2TOV3UPGRADE"))
	pflag.Bool("test-webhook", true, "run webhook tests")
	checkEnvVarBindingError(viper.BindEnv("test-webhook", viper.GetEnvPrefix()+"_WEBHOOK"))

	pflag.Bool("circuit-breaker", true, "enable circuit breaker to halt tests on infrastructure failures")
	checkEnvVarBindingError(viper.BindEnv("circuit-breaker", viper.GetEnvPrefix()+"_CIRCUIT_BREAKER"))
	pflag.Int("circuit-breaker-threshold", 3, "consecutive test failures before health-checking for infrastructure problems")
	checkEnvVarBindingError(viper.BindEnv("circuit-breaker-threshold", viper.GetEnvPrefix()+"_CIRCUIT_BREAKER_THRESHOLD"))

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
		shortEventuallyTimeout:          viper.GetDuration("shortEventuallyTimeout"),
		mediumEventuallyTimeout:         viper.GetDuration("mediumEventuallyTimeout"),
		longEventuallyTimeout:           viper.GetDuration("longEventuallyTimeout"),
		defaultEventuallyPollInterval:   viper.GetDuration("defaultEventuallyPollInterval"),
		defaultConsistentlyTimeout:      viper.GetDuration("defaultConsistentlyTimeout"),
		defaultConsistentlyPollInterval: viper.GetDuration("defaultConsistentlyPollInterval"),
	}
	testOpts.tag = viper.GetString("tag")
	if !slices.Contains(allowedTags, TestTag(testOpts.tag)) {
		fmt.Printf("Unknown tag: %s. Valid tags are: %v\n", testOpts.tag, allowedTags)
		os.Exit(1)
	}
	testOpts.operatorNamespace = viper.GetString("operator-namespace")
	testOpts.appsNamespace = viper.GetString("applications-namespace")
	testOpts.workbenchesNamespace = viper.GetString("workbenches-namespace")
	testOpts.monitoringNamespace = viper.GetString("dsc-monitoring-namespace")
	var err error
	if testOpts.deletionPolicy, err = ParseDeletionPolicy(viper.GetString("deletion-policy")); err != nil {
		fmt.Print(err.Error())
		os.Exit(1)
	}
	testOpts.cleanUpPreviousResources = viper.GetBool("clean-up-previous-resources")
	testOpts.operatorControllerTest = viper.GetBool("test-operator-controller")
	testOpts.dependantOperatorsManagementTest = viper.GetBool("test-dependant-operators-management")
	testOpts.dscManagementTest = viper.GetBool("test-dsc-management")
	testOpts.dscValidationTest = viper.GetBool("test-dsc-validation")
	testOpts.operatorResilienceTest = viper.GetBool("test-operator-resilience")
	testOpts.v2tov3upgradeTest = viper.GetBool("test-operator-v2tov3upgrade")
	testOpts.webhookTest = viper.GetBool("test-webhook")
	testOpts.circuitBreakerEnabled = viper.GetBool("circuit-breaker")
	testOpts.circuitBreakerThreshold = viper.GetInt("circuit-breaker-threshold")
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
		userv1.AddToScheme,
		apiextv1.AddToScheme,
		autoscalingv1.AddToScheme,
		dsciv2.AddToScheme,
		dscv2.AddToScheme,
		featurev1.AddToScheme,
		monitoringv1.AddToScheme,
		ofapi.AddToScheme,
		operatorv1.AddToScheme,
		componentApi.AddToScheme,
		serviceApi.AddToScheme,
		ofapiv1.AddToScheme,
		infrav1.AddToScheme,
	}

	for _, schemeFn := range schemes {
		utilruntime.Must(schemeFn(Scheme))
	}
}

// mustRun executes a test and records its result to the circuit breaker.
// After each failure, the failure classifier (run by HandleTestFailure) determines
// whether the failure is infrastructure-related or test-logic. Only infrastructure
// failures are recorded to the circuit breaker's consecutive failure counter.
//
// Running tests in parallel with WithParallel makes t.Run return before the
// subtest finishes, so results from parallel subtests are not recorded.
func mustRun(t *testing.T, name string, testFunc func(t *testing.T), opts ...TestCaseOpts) {
	t.Helper()

	if circuitBreaker.IsOpen() {
		t.Run(name, func(t *testing.T) {
			t.Helper()
			circuitBreaker.SkipIfOpen(t)
		})
		return
	}

	parallel := len(opts) > 0
	testExecuted := false
	wasSkipped := false

	passed := t.Run(name, func(t *testing.T) {
		testExecuted = true
		for _, opt := range opts {
			opt(t)
		}
		defer HandleGlobalPanic()
		defer func() { wasSkipped = t.Skipped() }()
		testFunc(t)
	})

	if !passed {
		HandleTestFailure(name)
		t.Logf("Stopping: %s test failed.", name)
		t.Fail()
	}

	if testExecuted && !parallel && !wasSkipped {
		circuitBreaker.RecordResult(passed, &lastClassification)
	}
}

func checkEnvVarBindingError(err error) {
	if err != nil {
		fmt.Printf("Error in binding tests env var: %s", err.Error())
		os.Exit(1)
	}
}
