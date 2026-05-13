package cloudmanager

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	ctrlmanager "sigs.k8s.io/controller-runtime/pkg/manager"

	ccmcommon "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	opmanager "github.com/opendatahub-io/opendatahub-operator/v2/pkg/manager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"
)

// ControllerTestConfig holds cloud-provider-specific values needed by the shared test helpers.
type ControllerTestConfig struct {
	// CRDSubdir is the subdirectory under config/cloudmanager/ containing CRD bases
	// (e.g., "azure" or "coreweave").
	CRDSubdir string

	// NewReconciler registers the cloud-specific reconciler with the manager.
	NewReconciler func(context.Context, ctrl.Manager, *operatorconfig.CloudManagerConfig) error

	// NewCR creates a new CR instance with the given dependencies.
	NewCR func(ccmcommon.Dependencies) client.Object

	// InstanceName is the singleton CR instance name
	// (e.g., AzureKubernetesEngineInstanceName).
	InstanceName string

	// InfraLabel is the expected infrastructure label value
	// (e.g., "azurekubernetesengine").
	InfraLabel string

	// GVK is the GroupVersionKind of the cloud-specific CR.
	GVK schema.GroupVersionKind
}

const TestOperatorNamespace = "test-operator-ns"

var chartsNotFound bool

// RequireCharts fails the test with a helpful message if charts were not found during setup.
func RequireCharts(t *testing.T) {
	t.Helper()

	if chartsNotFound {
		t.Fatal("opt/charts not populated, run 'make get-manifests'")
	}
}

// SetupEnvTest creates a new envtest environment with CRDs loaded from
// config/cloudmanager/<crdSubdir>/crd/bases. Additional envt.OptionFn values
// (e.g. envt.WithManager, envt.WithRegisterControllers) are forwarded to
// envt.New so the caller can opt into a manager that matches the production
// bootstrap (Unstructured cache, opmanager wrapper).
func SetupEnvTest(crdSubdir string, opts ...envt.OptionFn) (*envt.EnvT, error) {
	rootPath, err := envtestutil.FindProjectRoot()
	if err != nil {
		return nil, fmt.Errorf("failed to find project root: %w", err)
	}

	base := make([]envt.OptionFn, 0, 1+len(opts))
	base = append(base, envt.WithCRDPaths(
		filepath.Join(rootPath, "config", "cloudmanager", crdSubdir, "crd", "bases"),
	))

	return envt.New(append(base, opts...)...)
}

// NewTestContext creates a testf.TestContext configured for the given envtest environment.
func NewTestContext(ctx context.Context, et *envt.EnvT) (*testf.TestContext, error) {
	return testf.NewTestContext(
		testf.WithRestConfig(et.Config()),
		testf.WithScheme(et.Scheme()),
		testf.WithContext(ctx),
		testf.WithTOptions(
			testf.WithEventuallyTimeout(2*time.Minute),
			testf.WithEventuallyPollingInterval(250*time.Millisecond),
		),
	)
}

// StartManager creates the operator namespace and starts the EnvT-provided
// manager. The manager must have been created during SetupEnvTest (via
// envt.WithManager / envt.WithRegisterControllers) so it uses the same
// Unstructured cache and opmanager wrapper as production.
//
// Startup errors are propagated: the function blocks until the manager is
// elected (ready) or returns immediately if Start fails.
func StartManager(ctx context.Context, et *envt.EnvT) error {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: TestOperatorNamespace,
		},
	}
	if err := et.Client().Create(ctx, namespace); err != nil {
		return fmt.Errorf("failed to create namespace: %w", err)
	}

	mgr := et.Manager()

	errCh := make(chan error, 1)
	go func() {
		errCh <- mgr.Start(ctx)
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("manager stopped unexpectedly: %w", err)
	case <-mgr.Elected():
		return nil
	}
}

// StartIsolatedController starts a fresh, isolated test environment with the controller
// from cfg running. Each call creates its own Kubernetes API server. Use this for tests
// that need specific cluster state (e.g., no cert-manager CRDs installed).
func StartIsolatedController(t *testing.T, ctx context.Context, cfg ControllerTestConfig) (*envt.EnvT, *testf.WithT) {
	t.Helper()

	RequireCharts(t)

	rootPath, pathErr := envtestutil.FindProjectRoot()
	if pathErr != nil {
		t.Fatalf("failed to find project root: %v", pathErr)
	}

	chartsPath := filepath.Join(rootPath, "opt", "charts")

	et, err := SetupEnvTest(cfg.CRDSubdir,
		envt.WithManager(ctrl.Options{
			Controller: ctrlconfig.Controller{SkipNameValidation: ptr.To(true)},
		}),
		envt.WithOpManagerOptions(opmanager.WithChartsBasePath(chartsPath)),
		envt.WithRegisterControllers(func(mgr ctrlmanager.Manager) error {
			return cfg.NewReconciler(ctx, mgr, &operatorconfig.CloudManagerConfig{
				RhaiOperatorNamespace: TestOperatorNamespace,
				DefaultChartsPath:     chartsPath,
			})
		}),
	)
	if err != nil {
		t.Fatalf("failed to create isolated test environment: %v", err)
	}
	t.Cleanup(func() { _ = et.Stop() })

	isolatedTC, err := NewTestContext(ctx, et)
	if err != nil {
		t.Fatalf("failed to create test context: %v", err)
	}

	if err := StartManager(ctx, et); err != nil {
		t.Fatalf("failed to start manager: %v", err)
	}

	return et, isolatedTC.NewWithT(t)
}

// CreateCR creates the cloud-specific CR using cfg.NewCR and registers cleanup.
func CreateCR(t *testing.T, wt *testf.WithT, cfg ControllerTestConfig, deps ccmcommon.Dependencies) {
	t.Helper()

	obj := cfg.NewCR(deps)
	wt.Expect(wt.Client().Create(wt.Context(), obj)).Should(gomega.Succeed())
	envt.CleanupDelete(t, gomega.NewWithT(t), wt.Context(), wt.Client(), obj)
}

// ListInfraDeployments returns the Deployments with the InfrastructurePartOf
// label matching the given infraLabel in the specified namespace.
func ListInfraDeployments(wt *testf.WithT, namespace, infraLabel string) ([]unstructured.Unstructured, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk.Deployment.GroupVersion().WithKind(gvk.Deployment.Kind + "List"))

	if err := wt.Client().List(wt.Context(), list,
		client.InNamespace(namespace),
		client.MatchingLabels{
			labels.InfrastructurePartOf: labels.NormalizePartOfValue(infraLabel),
		},
	); err != nil {
		return nil, err
	}

	return list.Items, nil
}

// HasInfraDeployments returns true if there are Deployments with the InfrastructurePartOf
// label matching the given infraLabel in the specified namespace.
func HasInfraDeployments(wt *testf.WithT, namespace, infraLabel string) bool {
	items, err := ListInfraDeployments(wt, namespace, infraLabel)
	wt.Expect(err).NotTo(gomega.HaveOccurred())
	return len(items) > 0
}

// RunTestMain executes the common TestMain boilerplate for cloud controller integration tests.
// It sets up the charts path, envtest environment, test context, and manager, then runs
// the tests. The tc pointer is populated before tests run so it can be used as a
// package-level variable.
//
// Usage in suite_test.go:
//
//	var tc *testf.TestContext
//	func TestMain(m *testing.M) {
//	    cloudmanager.RunTestMain(m, &tc, cfg)
//	}
func RunTestMain(m *testing.M, tc **testf.TestContext, cfg ControllerTestConfig) {
	logf.SetLogger(zap.New(zap.UseDevMode(true)))

	rootPath, pathErr := envtestutil.FindProjectRoot()
	if pathErr != nil {
		logf.Log.Error(pathErr, "failed to find project root")
		os.Exit(1)
	}

	chartsPath := filepath.Join(rootPath, "opt", "charts")
	entries, err := os.ReadDir(chartsPath)
	if err != nil && !os.IsNotExist(err) {
		logf.Log.Error(err, "failed to read opt/charts directory")
		os.Exit(1)
	}
	if len(entries) == 0 || (len(entries) == 1 && entries[0].Name() == ".gitkeep") {
		logf.Log.Info("opt/charts is not populated, run 'make get-manifests' first; skipping controller tests")
		chartsNotFound = true
		os.Exit(m.Run())
	}

	ctx, cancel := context.WithCancel(context.Background())

	et, err := SetupEnvTest(cfg.CRDSubdir,
		envt.WithOpManagerOptions(opmanager.WithChartsBasePath(chartsPath)),
		envt.WithRegisterControllers(func(mgr ctrlmanager.Manager) error {
			return cfg.NewReconciler(ctx, mgr, &operatorconfig.CloudManagerConfig{
				RhaiOperatorNamespace: TestOperatorNamespace,
				DefaultChartsPath:     chartsPath,
			})
		}),
	)
	if err != nil {
		logf.Log.Error(err, "failed to setup test environment")
		os.Exit(1)
	}

	testCtx, err := NewTestContext(ctx, et)
	if err != nil {
		logf.Log.Error(err, "failed to create test context")
		os.Exit(1)
	}

	*tc = testCtx

	if err := StartManager(ctx, et); err != nil {
		logf.Log.Error(err, "failed to start manager")
		os.Exit(1)
	}

	code := m.Run()

	cancel()
	if err := et.Stop(); err != nil {
		logf.Log.Error(err, "failed to stop test environment")
	}

	os.Exit(code)
}
