package e2e_test

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	routev1 "github.com/openshift/api/route/v1"
	ofapi "github.com/operator-framework/api/pkg/operators/v1alpha1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	k8sclient "k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlruntime "sigs.k8s.io/controller-runtime/pkg/client/config"

	dsc "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	featurev1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/features/v1"
)

var (
	opNamespace  string
	skipDeletion bool
	scheme       = runtime.NewScheme()
)

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
	// time required to create a resource
	resourceCreationTimeout time.Duration
	// test DataScienceCluster instance
	testDsc *dsc.DataScienceCluster
	// test DSCI CR because we do not create it in ODH by default
	testDSCI *dsci.DSCInitialization
	// time interval to check for resource creation
	resourceRetryInterval time.Duration
	// context for accessing resources
	ctx context.Context
}

func NewTestContext() (*testContext, error) { //nolint:golint,revive // Only used in tests
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
	custClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize custom client: %w", err)
	}

	// setup DSCI CR since we do not create automatically by operator
	testDSCI := setupDSCICR("e2e-test-dsci")
	// Setup DataScienceCluster CR
	testDSC := setupDSCInstance("e2e-test")

	return &testContext{
		cfg:                     config,
		kubeClient:              kc,
		customClient:            custClient,
		operatorNamespace:       opNamespace,
		applicationsNamespace:   testDSCI.Spec.ApplicationsNamespace,
		resourceCreationTimeout: time.Minute * 2,
		resourceRetryInterval:   time.Second * 10,
		ctx:                     context.TODO(),
		testDsc:                 testDSC,
		testDSCI:                testDSCI,
	}, nil
}

// TestOdhOperator sets up the testing suite for ODH Operator.
func TestOdhOperator(t *testing.T) {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(routev1.AddToScheme(scheme))
	utilruntime.Must(apiextv1.AddToScheme(scheme))
	utilruntime.Must(autoscalingv1.AddToScheme(scheme))
	utilruntime.Must(dsci.AddToScheme(scheme))
	utilruntime.Must(dsc.AddToScheme(scheme))
	utilruntime.Must(featurev1.AddToScheme(scheme))
	utilruntime.Must(monitoringv1.AddToScheme(scheme))
	utilruntime.Must(ofapi.AddToScheme(scheme))

	// individual test suites after the operator is running
	if !t.Run("validate operator pod is running", testODHOperatorValidation) {
		return
	}
	// Run create and delete tests for all the components
	t.Run("create Opendatahub components", creationTestSuite)

	// Run deletion if skipDeletion is not set
	if !skipDeletion {
		// This test case recreates entire DSC again and deletes afterward
		// this is a negative test case, since by using the positive CM('true'), even CSV gets deleted which leaves no operator pod in prow
		t.Run("components should not be removed if labeled is set to 'false' on configmap", cfgMapDeletionTestSuite)

		t.Run("delete components", deletionTestSuite)
	}
}

func TestMain(m *testing.M) {
	// call flag.Parse() here if TestMain uses flags
	flag.StringVar(&opNamespace, "operator-namespace",
		"opendatahub-operator-system", "Namespace where the odh operator is deployed")
	flag.BoolVar(&skipDeletion, "skip-deletion", false, "skip deletion of the controllers")

	flag.Parse()
	os.Exit(m.Run())
}
