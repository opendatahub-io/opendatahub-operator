package coreweave_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/coreweave"
	testscheme "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"
)

var tc *testf.TestContext

func TestMain(m *testing.M) {
	ctx, cancel := context.WithCancel(context.Background())

	logf.SetLogger(zap.New(zap.WriteTo(io.Discard), zap.UseDevMode(true)))

	rootPath, pathErr := envtestutil.FindProjectRoot()
	if pathErr != nil {
		logf.Log.Error(pathErr, "failed to find project root")
		os.Exit(1)
	}

	// Point DefaultChartsPath to the real charts bundled in opt/charts.
	chartsPath := filepath.Join(rootPath, "opt", "charts")
	if _, err := os.Stat(chartsPath); os.IsNotExist(err) {
		logf.Log.Error(err, "opt/charts directory not found, run 'make get-manifests' first")
		os.Exit(1)
	}

	common.DefaultChartsPath = chartsPath

	s, err := testscheme.New()
	if err != nil {
		logf.Log.Error(err, "failed to create scheme")
		os.Exit(1)
	}

	envTestEnv := &envtest.Environment{
		CRDInstallOptions: envtest.CRDInstallOptions{
			Scheme: s,
			Paths: []string{
				filepath.Join(rootPath, "config", "cloudmanager", "coreweave", "crd", "bases"),
			},
			ErrorIfPathMissing: true,
			CleanUpAfterUse:    false,
		},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := envTestEnv.Start()
	if err != nil {
		logf.Log.Error(err, "failed to start test environment")
		os.Exit(1)
	}

	tc, err = testf.NewTestContext(
		testf.WithRestConfig(cfg),
		testf.WithScheme(s),
		testf.WithContext(ctx),
		testf.WithTOptions(
			testf.WithEventuallyTimeout(2*time.Minute),
			testf.WithEventuallyPollingInterval(250*time.Millisecond),
		),
	)
	if err != nil {
		logf.Log.Error(err, "failed to create test context")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:         s,
		LeaderElection: false,
		Metrics: ctrlmetrics.Options{
			BindAddress: "0",
		},
	})
	if err != nil {
		logf.Log.Error(err, "failed to create manager")
		os.Exit(1)
	}

	if err := coreweave.NewReconciler(ctx, mgr); err != nil {
		logf.Log.Error(err, "failed to create reconciler")
		os.Exit(1)
	}

	go func() {
		if err := mgr.Start(ctx); err != nil {
			logf.Log.Error(err, "failed to start manager")
		}
	}()

	code := m.Run()

	cancel()
	if err := envTestEnv.Stop(); err != nil {
		logf.Log.Error(err, "failed to stop test environment")
	}

	os.Exit(code)
}
