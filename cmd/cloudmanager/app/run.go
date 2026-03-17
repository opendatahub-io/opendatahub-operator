package app

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	certmanager "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/dependency/certmanager"
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

	if err := validateRequiredEnvVars(); err != nil {
		return err
	}

	scheme := newScheme(provider.AddToScheme)

	clientOptions := provider.ClientOptions()
	if clientOptions.Cache == nil {
		clientOptions.Cache = &client.CacheOptions{}
	}
	// The unstructured cache must be used.
	clientOptions.Cache.Unstructured = true

	cacheOptions, err := provider.CacheOptions(scheme)
	if err != nil {
		return fmt.Errorf("unable to get cache options: %w", err)
	}

	mgrOpts := ctrl.Options{
		Scheme: scheme,
		Metrics: ctrlmetrics.Options{
			BindAddress: cfg.MetricsAddr,
		},
		// This is the default mapper provider, we define it to ensure it remains
		// consistent with controller-runtime updates. It is needed for the action dynamicownership.
		MapperProvider:         apiutil.NewDynamicRESTMapper,
		HealthProbeBindAddress: cfg.HealthProbeAddr,
		LeaderElection:         cfg.LeaderElection,
		LeaderElectionID:       provider.LeaderElectionID,
		Cache:                  cacheOptions,
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

// requiredEnvVars lists environment variables that must be set for any cloud manager provider.
var requiredEnvVars = []string{
	certmanager.EnvOperatorNamespace,
}

func validateRequiredEnvVars() error {
	for _, env := range requiredEnvVars {
		if os.Getenv(env) == "" {
			return fmt.Errorf("required environment variable %s is not set", env)
		}
	}
	return nil
}

func newScheme(addToSchemes ...func(*runtime.Scheme) error) *runtime.Scheme {
	scheme := runtime.NewScheme()

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))

	for _, addToScheme := range addToSchemes {
		utilruntime.Must(addToScheme(scheme))
	}

	return scheme
}
