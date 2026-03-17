package coreweave_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/common"
	testscheme "github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/scheme"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"
)

var (
	tc             *testf.TestContext
	chartsNotFound bool
)

func requireCharts(t *testing.T) {
	t.Helper()

	if chartsNotFound {
		t.Fatal("opt/charts not populated, run 'make get-manifests'")
	}
}

func TestMain(m *testing.M) {
	logf.SetLogger(zap.New(zap.WriteTo(io.Discard), zap.UseDevMode(true)))

	teardown := setupEnv()
	code := m.Run()
	teardown()

	os.Exit(code)
}

func setupEnv() func() {
	rootPath, pathErr := envtestutil.FindProjectRoot()
	if pathErr != nil {
		logf.Log.Error(pathErr, "failed to find project root")
		os.Exit(1)
	}

	// Point DefaultChartsPath to the real charts bundled in opt/charts.
	chartsPath := filepath.Join(rootPath, "opt", "charts")
	entries, err := os.ReadDir(chartsPath)
	if err != nil && !os.IsNotExist(err) {
		logf.Log.Error(err, "failed to read opt/charts directory")
		os.Exit(1)
	}
	if len(entries) == 0 || (len(entries) == 1 && entries[0].Name() == ".gitkeep") {
		logf.Log.Error(errors.New("opt/charts is not populated"), "run 'make get-manifests' first")
		chartsNotFound = true

		return func() {}
	}

	ctx, cancel := context.WithCancel(context.Background())

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

	tc, err = newCoreweaveTestContext(ctx, cfg, s)
	if err != nil {
		logf.Log.Error(err, "failed to create test context")
		os.Exit(1)
	}

	if err := startCoreweaveManager(ctx, cfg, s, false); err != nil {
		logf.Log.Error(err, "failed to start manager")
		os.Exit(1)
	}

	return func() {
		cancel()
		if err := envTestEnv.Stop(); err != nil {
			logf.Log.Error(err, "failed to stop test environment")
		}
	}
}
