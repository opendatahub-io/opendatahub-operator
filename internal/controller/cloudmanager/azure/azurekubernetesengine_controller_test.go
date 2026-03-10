package azure_test

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/config"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/azure/v1alpha1"
	ccmcommon "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/azure"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

func TestAzureKubernetesEngine(t *testing.T) {
	t.Run("deploys managed dependencies", func(t *testing.T) {
		wt := tc.NewWithT(t)

		createAzureCR(t, wt, ccmcommon.Dependencies{
			CertManager:  ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
			LWS:          ccmcommon.LWSDependency{ManagementPolicy: ccmcommon.Managed},
			SailOperator: ccmcommon.SailOperatorDependency{ManagementPolicy: ccmcommon.Managed},
		})

		nn := types.NamespacedName{Name: ccmv1alpha1.AzureKubernetesEngineInstanceName}

		// Wait for reconciliation to succeed
		wt.Get(gvk.AzureKubernetesEngine, nn).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`),
		)

		// Verify dependency deployments are created
		wt.Get(gvk.Deployment, types.NamespacedName{
			Name: "cert-manager-operator-controller-manager", Namespace: "cert-manager-operator",
		}).Eventually().Should(Not(BeNil()))

		wt.Get(gvk.Deployment, types.NamespacedName{
			Name: "openshift-lws-operator", Namespace: "openshift-lws-operator",
		}).Eventually().Should(Not(BeNil()))

		wt.Get(gvk.Deployment, types.NamespacedName{
			Name: "servicemesh-operator3", Namespace: "istio-system",
		}).Eventually().Should(Not(BeNil()))
	})

	t.Run("sets infrastructure label on deployed resources", func(t *testing.T) {
		wt := tc.NewWithT(t)

		createAzureCR(t, wt, ccmcommon.Dependencies{
			CertManager: ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
		})

		nn := types.NamespacedName{Name: ccmv1alpha1.AzureKubernetesEngineInstanceName}

		wt.Get(gvk.AzureKubernetesEngine, nn).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`),
		)

		wt.Get(gvk.Deployment, types.NamespacedName{
			Name: "cert-manager-operator-controller-manager", Namespace: "cert-manager-operator",
		}).Eventually().Should(
			jq.Match(`.metadata.labels."%s" == "azurekubernetesengine"`, labels.InfrastructurePartOf),
		)
	})

	t.Run("creates PKI bootstrap resources when cert-manager is installed", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		et, wtC := startIsolatedAzureController(t, ctx)
		t.Cleanup(cancel) // stop the manager before the test environment (registered after et.Stop, so it runs first)

		_, err := et.RegisterCertManagerCRDs(ctx, envt.WithPermissiveSchema())
		wtC.Expect(err).NotTo(HaveOccurred())

		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "cert-manager"}}
		if err := et.Client().Create(ctx, ns); err != nil && !k8serr.IsAlreadyExists(err) {
			wtC.Expect(err).NotTo(HaveOccurred())
		}

		createAzureCR(t, wtC, ccmcommon.Dependencies{
			CertManager:  ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
			LWS:          ccmcommon.LWSDependency{ManagementPolicy: ccmcommon.Managed},
			SailOperator: ccmcommon.SailOperatorDependency{ManagementPolicy: ccmcommon.Managed},
		})

		nn := types.NamespacedName{Name: ccmv1alpha1.AzureKubernetesEngineInstanceName}

		wtC.Get(gvk.CertManagerClusterIssuer, types.NamespacedName{Name: "opendatahub-selfsigned-issuer"}).
			Eventually().ShouldNot(BeNil())
		wtC.Get(gvk.CertManagerCertificate, types.NamespacedName{Name: "opendatahub-ca", Namespace: "cert-manager"}).
			Eventually().ShouldNot(BeNil())
		wtC.Get(gvk.CertManagerClusterIssuer, types.NamespacedName{Name: "opendatahub-ca-issuer"}).
			Eventually().ShouldNot(BeNil())

		wtC.Get(gvk.AzureKubernetesEngine, nn).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "DependenciesAvailable") | .status == "True"`),
		)
		wtC.Get(gvk.AzureKubernetesEngine, nn).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "True"`),
		)
	})
}

// TestAzureKubernetesEngineWithoutCertManager tests cert-manager CRD absence and dynamic
// registration. Each sub-test uses an isolated envtest to start with zero cert-manager CRDs.
func TestAzureKubernetesEngineWithoutCertManager(t *testing.T) {
	logf.SetLogger(zap.New(zap.WriteTo(io.Discard), zap.UseDevMode(true)))

	t.Run("reports DependenciesAvailable=False and Ready=False when cert-manager CRDs absent", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		et, wtC := startIsolatedAzureController(t, ctx)
		t.Cleanup(cancel) // stop the manager before the test environment (registered after et.Stop, so it runs first)
		_ = et

		nn := types.NamespacedName{Name: ccmv1alpha1.AzureKubernetesEngineInstanceName}
		createAzureCR(t, wtC, ccmcommon.Dependencies{
			CertManager: ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
		})

		wtC.Get(gvk.AzureKubernetesEngine, nn).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "DependenciesAvailable") | .status == "False"`),
		)
		wtC.Get(gvk.AzureKubernetesEngine, nn).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "Ready") | .status == "False"`),
		)
	})

	t.Run("reconciles PKI resources after cert-manager CRDs appear", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		et, wtC := startIsolatedAzureController(t, ctx)
		t.Cleanup(cancel) // stop the manager before the test environment (registered after et.Stop, so it runs first)
		nn := types.NamespacedName{Name: ccmv1alpha1.AzureKubernetesEngineInstanceName}

		createAzureCR(t, wtC, ccmcommon.Dependencies{
			CertManager: ccmcommon.CertManagerDependency{ManagementPolicy: ccmcommon.Managed},
		})

		wtC.Get(gvk.AzureKubernetesEngine, nn).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "DependenciesAvailable") | .status == "False"`),
		)

		_, err := et.RegisterCertManagerCRDs(ctx, envt.WithPermissiveSchema())
		wtC.Expect(err).NotTo(HaveOccurred())

		ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "cert-manager"}}
		if err := et.Client().Create(ctx, ns); err != nil && !k8serr.IsAlreadyExists(err) {
			wtC.Expect(err).NotTo(HaveOccurred())
		}

		wtC.Get(gvk.AzureKubernetesEngine, nn).Eventually().Should(
			jq.Match(`.status.conditions[] | select(.type == "DependenciesAvailable") | .status == "True"`),
		)
		wtC.Get(gvk.CertManagerClusterIssuer, types.NamespacedName{Name: "opendatahub-selfsigned-issuer"}).
			Eventually().ShouldNot(BeNil())
		wtC.Get(gvk.CertManagerCertificate, types.NamespacedName{Name: "opendatahub-ca", Namespace: "cert-manager"}).
			Eventually().ShouldNot(BeNil())
		wtC.Get(gvk.CertManagerClusterIssuer, types.NamespacedName{Name: "opendatahub-ca-issuer"}).
			Eventually().ShouldNot(BeNil())
	})
}

// startIsolatedAzureController starts a fresh, isolated test environment with the Azure
// controller running. Each call creates its own Kubernetes API server, completely separate
// from the shared suite environment. Use this for tests that need specific cluster state
// (e.g., no cert-manager CRDs installed).
func startIsolatedAzureController(t *testing.T, ctx context.Context) (*envt.EnvT, *testf.WithT) {
	t.Helper()

	et, err := envt.New()
	if err != nil {
		t.Fatalf("failed to create isolated test environment: %v", err)
	}
	t.Cleanup(func() { _ = et.Stop() })

	isolatedTC, err := newAzureTestContext(ctx, et.Config(), et.Scheme())
	if err != nil {
		t.Fatalf("failed to create test context: %v", err)
	}

	// skipNameValidation=true: the Azure controller name is already registered by the main suite.
	if err := startAzureManager(ctx, et.Config(), et.Scheme(), true); err != nil {
		t.Fatalf("failed to start azure manager: %v", err)
	}

	return et, isolatedTC.NewWithT(t)
}

func newAzureTestContext(ctx context.Context, cfg *rest.Config, s *runtime.Scheme) (*testf.TestContext, error) {
	return testf.NewTestContext(
		testf.WithRestConfig(cfg),
		testf.WithScheme(s),
		testf.WithContext(ctx),
		testf.WithTOptions(
			testf.WithEventuallyTimeout(2*time.Minute),
			testf.WithEventuallyPollingInterval(250*time.Millisecond),
		),
	)
}

// startAzureManager creates a controller-runtime manager with the Azure reconciler registered
// and starts it in the background. The manager runs until ctx is cancelled.
// Set skipNameValidation to true when the same controller name is already registered
// (e.g., when the main suite has already started the controller).
func startAzureManager(ctx context.Context, cfg *rest.Config, s *runtime.Scheme, skipNameValidation bool) error {
	opts := ctrl.Options{
		Scheme:         s,
		LeaderElection: false,
		Metrics:        ctrlmetrics.Options{BindAddress: "0"},
	}
	if skipNameValidation {
		opts.Controller = ctrlconfig.Controller{SkipNameValidation: ptr.To(true)}
	}

	mgr, err := ctrl.NewManager(cfg, opts)
	if err != nil {
		return fmt.Errorf("create manager: %w", err)
	}
	if err := azure.NewReconciler(ctx, mgr); err != nil {
		return fmt.Errorf("register reconciler: %w", err)
	}
	go func() {
		if err := mgr.Start(ctx); err != nil {
			logf.Log.Error(err, "manager stopped with error")
		}
	}()
	return nil
}

func createAzureCR(t *testing.T, wt *testf.WithT, deps ccmcommon.Dependencies) {
	t.Helper()

	ake := &ccmv1alpha1.AzureKubernetesEngine{
		ObjectMeta: metav1.ObjectMeta{
			Name: ccmv1alpha1.AzureKubernetesEngineInstanceName,
		},
		Spec: ccmv1alpha1.AzureKubernetesEngineSpec{
			Dependencies: deps,
		},
	}

	wt.Expect(wt.Client().Create(wt.Context(), ake)).Should(Succeed())
	envt.CleanupDelete(t, NewWithT(t), wt.Context(), wt.Client(), ake)
}
