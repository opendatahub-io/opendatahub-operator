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

// OperatorConfig defines the operator manager configuration loaded from environment
// variables and flags via Viper.
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

// LoadConfig loads complete operator configuration including flags parsing and rest.Config loading.
// This is the main entry point for configuration initialization when using pflag directly (not cobra).
func LoadConfig() (*Config, error) {
	// Define flags and env vars
	if err := flags.AddOperatorFlagsAndEnvvars(viper.GetEnvPrefix()); err != nil {
		return nil, fmt.Errorf("error adding flags or binding env vars: %w", err)
	}

	// Parse and bind flags
	pflag.Parse()
	if err := viper.BindPFlags(pflag.CommandLine); err != nil {
		return nil, fmt.Errorf("error binding flags: %w", err)
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal operator manager config: %w", err)
	}

	if err := setupConfig(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
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
