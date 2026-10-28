package main

import (
	"fmt"
	"os"

	frameworkmanager "github.com/opendatahub-io/odh-platform-utilities/framework/manager"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	syntheticv1alpha1 "github.com/opendatahub-io/opendatahub-operator/tests/e2e/syntheticmodule/api/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/tests/e2e/syntheticmodule/internal/controller"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zap.Options{Development: false})))
	ctx := ctrl.SetupSignalHandler()

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(syntheticv1alpha1.AddToScheme(scheme))

	config := ctrl.GetConfigOrDie()
	ctrlMgr, err := ctrl.NewManager(config, ctrl.Options{
		Scheme:                 scheme,
		MapperProvider:         apiutil.NewDynamicRESTMapper,
		HealthProbeBindAddress: ":8081",
		Metrics:                ctrlmetrics.Options{BindAddress: ":8443", SecureServing: true},
		LeaderElection:         true,
		LeaderElectionID:       "synthetic-module.platform.opendatahub.io",
		Client: client.Options{Cache: &client.CacheOptions{
			Unstructured: true,
		}},
	})
	if err != nil {
		return fmt.Errorf("create manager: %w", err)
	}

	mgr := frameworkmanager.New(ctrlMgr)
	if err := controller.NewReconciler(ctx, mgr); err != nil {
		return fmt.Errorf("create synthetic module reconciler: %w", err)
	}
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("add health check: %w", err)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("add readiness check: %w", err)
	}
	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("run manager: %w", err)
	}
	return nil
}
