package operatorconfig

import (
	"fmt"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/flags"
)

// Config holds common configuration shared by all binaries (main operator and cloudmanager).
type Config struct {
	MetricsAddr         string `mapstructure:"metrics-bind-address"`
	HealthProbeAddr     string `mapstructure:"health-probe-bind-address"`
	LeaderElection      bool   `mapstructure:"leader-elect"`
	MonitoringNamespace string `mapstructure:"dsc-monitoring-namespace"`
	LogMode             string `mapstructure:"log-mode"`
	PprofAddr           string `mapstructure:"pprof-bind-address"`

	// Zap logging configuration
	ZapDevel        bool   `mapstructure:"zap-devel"`
	ZapEncoder      string `mapstructure:"zap-encoder"`
	ZapLogLevel     string `mapstructure:"zap-log-level"`
	ZapStacktrace   string `mapstructure:"zap-stacktrace-level"`
	ZapTimeEncoding string `mapstructure:"zap-time-encoding"`

	// Kubernetes connection configuration
	RestConfig *rest.Config `mapstructure:"-"`
	// Zap logger options
	ZapOptions *zap.Options `mapstructure:"-"`
}

// OperatorSettings holds operator-specific settings for the main binary.
type OperatorSettings struct {
	OperatorNamespace string `mapstructure:"operator-namespace"`
	DisableDSCConfig  string `mapstructure:"disable-dsc-config"`
	ManifestsBasePath string `mapstructure:"default-manifests-path"`
	PlatformType      string `mapstructure:"platform-type"`
}

// IsDSCICreationDisabled returns true if automatic DSCI creation is disabled.
func (s OperatorSettings) IsDSCICreationDisabled() bool {
	return s.DisableDSCConfig != "" && s.DisableDSCConfig != "false"
}

// OperatorConfig holds the full configuration for the main operator binary.
type OperatorConfig struct {
	Config           `mapstructure:",squash"`
	OperatorSettings `mapstructure:",squash"`
}

// LoadConfig loads complete operator configuration including flags parsing and rest.Config loading.
func LoadConfig() (*OperatorConfig, error) {
	// Define flags and env vars
	if err := flags.AddOperatorFlagsAndEnvvars(viper.GetEnvPrefix()); err != nil {
		return nil, fmt.Errorf("error adding flags or binding env vars: %w", err)
	}

	// Parse and bind flags
	pflag.Parse()
	if err := viper.BindPFlags(pflag.CommandLine); err != nil {
		return nil, fmt.Errorf("error binding flags: %w", err)
	}

	var oc OperatorConfig
	if err := viper.Unmarshal(&oc); err != nil {
		return nil, fmt.Errorf("failed to unmarshal operator config: %w", err)
	}

	if err := setupConfig(&oc.Config); err != nil {
		return nil, err
	}

	return &oc, nil
}

// setupConfig loads the Kubernetes rest.Config and configures zap logger options
// on an already-unmarshalled Config. Shared by LoadConfig and BuildCloudManagerConfig.
func setupConfig(cfg *Config) error {
	restConfig, err := config.GetConfig()
	if err != nil {
		return fmt.Errorf("error getting rest config: %w", err)
	}
	cfg.RestConfig = restConfig

	zapFlagSet := flags.NewZapFlagSet()
	opts := &zap.Options{}
	opts.BindFlags(zapFlagSet)

	if err := flags.ParseZapFlags(
		zapFlagSet,
		cfg.ZapDevel,
		cfg.ZapEncoder,
		cfg.ZapLogLevel,
		cfg.ZapStacktrace,
		cfg.ZapTimeEncoding,
	); err != nil {
		return fmt.Errorf("error parsing zap flags: %w", err)
	}
	cfg.ZapOptions = opts

	return nil
}
