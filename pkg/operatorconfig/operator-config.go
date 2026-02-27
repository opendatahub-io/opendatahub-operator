package operatorconfig

import (
	"fmt"
	"strings"

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
// This is the main entry point for configuration initialization.
func LoadConfig() (*Config, error) {
	// Setup Viper
	viper.SetEnvPrefix("ODH_MANAGER")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	// Define flags and env vars
	if err := flags.AddOperatorFlagsAndEnvvars(viper.GetEnvPrefix()); err != nil {
		return nil, fmt.Errorf("error adding flags or binding env vars: %w", err)
	}

	// Parse and bind flags
	pflag.Parse()
	if err := viper.BindPFlags(pflag.CommandLine); err != nil {
		return nil, fmt.Errorf("error binding flags: %w", err)
	}

	// Unmarshal configuration from Viper
	var operatorConfig Config
	if err := viper.Unmarshal(&operatorConfig); err != nil {
		return nil, fmt.Errorf("failed to unmarshal operator manager config: %w", err)
	}

	// Load Kubernetes rest.Config
	restConfig, err := config.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("error getting rest config: %w", err)
	}
	operatorConfig.RestConfig = restConfig

	// Configure zap logger options
	zapFlagSet := flags.NewZapFlagSet()
	opts := &zap.Options{}
	opts.BindFlags(zapFlagSet)

	if err := flags.ParseZapFlags(
		zapFlagSet,
		operatorConfig.ZapDevel,
		operatorConfig.ZapEncoder,
		operatorConfig.ZapLogLevel,
		operatorConfig.ZapStacktrace,
		operatorConfig.ZapTimeEncoding,
	); err != nil {
		return nil, fmt.Errorf("error parsing zap flags: %w", err)
	}
	operatorConfig.ZapOptions = opts

	return &operatorConfig, nil
}
