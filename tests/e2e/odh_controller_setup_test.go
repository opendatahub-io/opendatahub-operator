package e2e

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	kfdefappskubefloworgv1 "github.com/opendatahub-io/opendatahub-operator/apis/kfdef.apps.kubeflow.org/v1"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/pkg/errors"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	k8sclient "k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlruntime "sigs.k8s.io/controller-runtime/pkg/client/config"
)

var (
	kfdefTestNamespace string
	skipDeletion       bool
	scheme             = runtime.NewScheme()
)

// Holds information specific to individual tests
type testContext struct {
	// Rest config
	cfg *rest.Config
	// client for k8s resources
	kubeClient *k8sclient.Clientset
	// custom client for managing cutom resources
	customClient client.Client
	// namespace for running the tests
	testNamespace string
	// time rquired to create a resource
	resourceCreationTimeout time.Duration
	// time interval to check for resource creation
	resourceRetryInterval time.Duration
	// test KfDef for e2e
	testKfDefs []kfDefContext
	// context for accessing resources
	ctx context.Context
}

// kfDefContext holds information about test resource
// Any KfDef that needs to be added to the e2e test suite should be defined in
// the kfDefContext struct.
type kfDefContext struct {
	// metadata for KfDef object
	kfObjectMeta *metav1.ObjectMeta
	// metadata for KfDef Spec
	kfSpec *kfdefappskubefloworgv1.KfDefSpec
}

func NewTestContext() (*testContext, error) {

	// GetConfig(): If KUBECONFIG env variable is set, it is used to create
	// the client, else the inClusterConfig() is used.
	// Lastly if none of the them are set, it uses  $HOME/.kube/config to create the client.
	config, err := ctrlruntime.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("error creating the config object %v", err)
	}

	kc, err := k8sclient.NewForConfig(config)
	if err != nil {
		return nil, errors.Wrap(err, "failed to initialize Kubernetes client")
	}

	// custom client to manages resources like KfDef, Route etc
	custClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return nil, errors.Wrap(err, "failed to initialize custom client")
	}

	// Setup all KfDefs.
	// Note: Multiple KfDefs can be added to this list for testing.
	testKfDefContextList := []kfDefContext{setupCoreKfdef()}

	return &testContext{
		cfg:           config,
		kubeClient:    kc,
		customClient:  custClient,
		testNamespace: kfdefTestNamespace,
		// Set high timeout for CI environment
		resourceCreationTimeout: time.Minute * 3,
		resourceRetryInterval:   time.Second * 10,
		ctx:                     context.TODO(),
		testKfDefs:              testKfDefContextList,
	}, nil
}

// TestOdhOperator sets up the testing suite for ODH Operator.
func TestOdhOperator(t *testing.T) {

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(kfdefappskubefloworgv1.AddToScheme(scheme))
	utilruntime.Must(routev1.AddToScheme(scheme))
	utilruntime.Must(apiextv1.AddToScheme(scheme))

	// individual test suites after the operator is running
	if !t.Run("validate controllers", testKfDefControllerValidation) {
		return
	}
	// Run create and delete tests for all the test KfDefs
	t.Run("create", creationTestSuite)
	if !skipDeletion {
		t.Run("delete", deletionTestSuite)
	}
}

func TestMain(m *testing.M) {
	//call flag.Parse() here if TestMain uses flags
	flag.StringVar(&kfdefTestNamespace, "kf-namespace",
		"e2e-odh-operator", "Custom namespace where the odh contollers are deployed")
	flag.BoolVar(&skipDeletion, "skip-deletion", false, "skip deletion of the controllers")
	flag.Parse()
	os.Exit(m.Run())
}
