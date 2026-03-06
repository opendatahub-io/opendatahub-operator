package app

import (
	"fmt"

	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/logger"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/manager"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/operatorconfig"
)

// Run starts the cloud manager operator for the given provider.
func Run(_ *cobra.Command, provider Provider) error {
	cfg, err := operatorconfig.BuildConfig()
	if err != nil {
		return fmt.Errorf("failed to build operator config: %w", err)
	}

	ctrl.SetLogger(logger.NewLogger(cfg.LogMode, cfg.ZapOptions))

	setupLog := ctrl.Log.WithName("setup")

	ctx := ctrl.SetupSignalHandler()
	ctx = logf.IntoContext(ctx, setupLog)

	if err := provider.Validate(); err != nil {
		return fmt.Errorf("invalid provider configuration: %w", err)
	}

	scheme := common.NewScheme(provider.AddToScheme)

	clientOptions := provider.ClientOptions()
	if clientOptions.Cache == nil {
		clientOptions.Cache = &client.CacheOptions{}
	}
	// The unstructured cache must be used.
	clientOptions.Cache.Unstructured = true

	mgrOpts := ctrl.Options{
		Scheme: scheme,
		Metrics: ctrlmetrics.Options{
			BindAddress: cfg.MetricsAddr,
		},
		MapperProvider:         apiutil.NewDynamicRESTMapper,
		HealthProbeBindAddress: cfg.HealthProbeAddr,
		LeaderElection:         cfg.LeaderElection,
		LeaderElectionID:       provider.LeaderElectionID,
		Cache:                  provider.CacheOptions(scheme),
		Client:                 clientOptions,
	}

	ctrlMgr, err := ctrl.NewManager(cfg.RestConfig, mgrOpts)
	if err != nil {
		return fmt.Errorf("unable to start manager: %w", err)
	}

	mgr := manager.New(ctrlMgr)

	if err := provider.NewReconciler(ctx, mgr); err != nil {
		return fmt.Errorf("unable to create %s cloud manager reconciler: %w", provider.Name, err)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up health check: %w", err)
	}

	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return fmt.Errorf("unable to set up ready check: %w", err)
	}

	setupLog.Info("starting cloud manager", "provider", provider.Name)

	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("problem running manager: %w", err)
	}

	return nil
}
