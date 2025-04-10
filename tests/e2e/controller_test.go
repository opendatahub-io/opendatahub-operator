package e2e_test

import (
	"context"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/onsi/gomega"
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
	k8sclient "k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlruntime "sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/api/features/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
)

type TestFn func(t *testing.T)

var (
	testOpts testContextConfig
	Scheme   = runtime.NewScheme()

	componentsTestSuites = map[string]TestFn{
		// do not add modelcontroller here, due to dependency, test it separately below
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
	}

	servicesTestSuites = map[string]TestFn{
		serviceApi.MonitoringServiceName: monitoringTestSuite,
		serviceApi.AuthServiceName:       authControllerTestSuite,
	}
)

type arrayFlags []string

func (i *arrayFlags) String() string {
	return fmt.Sprintf("%v", *i)
}

func (i *arrayFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}

type testContextConfig struct {
	operatorNamespace string
	skipDeletion      bool

	operatorControllerTest bool
	webhookTest            bool
	components             arrayFlags
	services               arrayFlags
}

// Holds information specific to individual tests.
type testContext struct {
	// Rest config
	cfg *rest.Config
	// client for k8s resources
	kubeClient *k8sclient.Clientset
	// custom client for managing custom resources
	customClient client.Client
	// namespace of the operator
	operatorNamespace string
	// namespace of the deployed applications
	applicationsNamespace string
	// test DataScienceCluster instance
	testDsc *dscv1.DataScienceCluster
	// test DSCI CR because we do not create it in ODH by default
	testDSCI *dsciv1.DSCInitialization
	// test platform
	platform common.Platform
	// context for accessing resources
	//nolint:containedctx //reason: legacy v1 test setup
	ctx context.Context
	// test configuration
	testOpts testContextConfig
}

func NewTestContext() (*testContext, error) {
	// GetConfig(): If KUBECONFIG env variable is set, it is used to create
	// the client, else the inClusterConfig() is used.
	// Lastly if none of them are set, it uses  $HOME/.kube/config to create the client.
	config, err := ctrlruntime.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("error creating the config object %w", err)
	}

	kc, err := k8sclient.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Kubernetes client: %w", err)
	}

	// custom client to manages resources like Route etc
	custClient, err := client.New(config, client.Options{Scheme: Scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize custom client: %w", err)
	}

	// setup DSCI CR since we do not create automatically by operator
	testDSCI := setupDSCICR("e2e-test-dsci")
	// Setup DataScienceCluster CR
	testDSC := setupDSCInstance("e2e-test-dsc")

	return &testContext{
		cfg:                   config,
		kubeClient:            kc,
		customClient:          custClient,
		operatorNamespace:     testOpts.operatorNamespace,
		applicationsNamespace: testDSCI.Spec.ApplicationsNamespace,
		ctx:                   context.TODO(),
		testDsc:               testDSC,
		testDSCI:              testDSCI,
		platform:              cluster.SelfManagedRhoai,
		testOpts:              testOpts,
	}, nil
}

// TestOdhOperator sets up the testing suite for ODH Operator.
func TestOdhOperator(t *testing.T) {
	utilruntime.Must(clientgoscheme.AddToScheme(Scheme))
	utilruntime.Must(routev1.AddToScheme(Scheme))
	utilruntime.Must(apiextv1.AddToScheme(Scheme))
	utilruntime.Must(autoscalingv1.AddToScheme(Scheme))
	utilruntime.Must(dsciv1.AddToScheme(Scheme))
	utilruntime.Must(dscv1.AddToScheme(Scheme))
	utilruntime.Must(featurev1.AddToScheme(Scheme))
	utilruntime.Must(monitoringv1.AddToScheme(Scheme))
	utilruntime.Must(ofapi.AddToScheme(Scheme))
	utilruntime.Must(operatorv1.AddToScheme(Scheme))
	utilruntime.Must(componentApi.AddToScheme(Scheme))
	utilruntime.Must(serviceApi.AddToScheme(Scheme))
	utilruntime.Must(ofapiv1.AddToScheme(Scheme))

	log.SetLogger(zap.New(zap.UseDevMode(true)))

	gomega.SetDefaultEventuallyTimeout(generalWaitTimeout)
	gomega.SetDefaultEventuallyPollingInterval(generalPollInterval)

	if testOpts.operatorControllerTest {
		// individual test suites after the operator is running
		if !t.Run("validate operator pod is running", testODHOperatorValidation) {
			return
		}
	}

	// Run create and delete tests for all the components
	t.Run("create DSCI and DSC CRs", creationTestSuite)

	t.Run("components", func(t *testing.T) {
		for k, v := range componentsTestSuites {
			if len(testOpts.components) != 0 && !slices.Contains(testOpts.components, k) {
				t.Logf("Skipping tests for component %s", k)
				continue
			}

			t.Run(k, v)
		}
	})

	t.Run("services", func(t *testing.T) {
		for k, v := range servicesTestSuites {
			if len(testOpts.services) != 0 && !slices.Contains(testOpts.services, k) {
				t.Logf("Skipping tests for services %s", k)
				continue
			}

			t.Run(k, v)
		}
	})

	// Run deletion if skipDeletion is not set
	if !testOpts.skipDeletion {
		if testOpts.operatorControllerTest {
			// this is a negative test case, since by using the positive CM('true'), even CSV gets deleted which leaves no operator pod in prow
			t.Run("components should not be removed if labeled is set to 'false' on configmap", cfgMapDeletionTestSuite)
		}

		t.Run("delete components", deletionTestSuite)
	}
}

func TestMain(m *testing.M) {
	// call flag.Parse() here if TestMain uses flags
	flag.StringVar(&testOpts.operatorNamespace, "operator-namespace", "opendatahub-operator-system", "Namespace where the odh operator is deployed")
	flag.BoolVar(&testOpts.skipDeletion, "skip-deletion", false, "skip deletion of the controllers")

	flag.BoolVar(&testOpts.operatorControllerTest, "test-operator-controller", true, "run operator controller tests")
	flag.BoolVar(&testOpts.webhookTest, "test-webhook", true, "run webhook tests")

	componentNames := strings.Join(maps.Keys(componentsTestSuites), ", ")
	flag.Var(&testOpts.components, "test-component", "run tests for the specified component. valid components names are: "+componentNames)

	for _, n := range testOpts.components {
		if _, ok := componentsTestSuites[n]; !ok {
			fmt.Printf("test-component: unknown component %s, valid values are: %s", n, componentNames)
			os.Exit(1)
		}
	}

	serviceNames := strings.Join(maps.Keys(servicesTestSuites), ", ")
	flag.Var(&testOpts.services, "test-service", "run tests for the specified service. valid service names are: "+serviceNames)

	for _, n := range testOpts.components {
		if _, ok := componentsTestSuites[n]; !ok {
			fmt.Printf("test-service: unknown service %s, valid values are: %s", n, serviceNames)
			os.Exit(1)
		}
	}

	flag.Parse()
	os.Exit(m.Run())
}
