package operatorconfig

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

const (
	// FlagRhaiOperatorNamespace is the viper/flag key for the operator namespace.
	FlagRhaiOperatorNamespace = "rhai-operator-namespace"
	// EnvRhaiOperatorNamespace is the environment variable name for the operator namespace.
	EnvRhaiOperatorNamespace = "RHAI_OPERATOR_NAMESPACE"

	// FlagDefaultChartsPath is the viper/flag key for the default Helm charts directory.
	FlagDefaultChartsPath = "default-charts-path"
	// EnvDefaultChartsPath is the environment variable name for the default Helm charts directory.
	EnvDefaultChartsPath = "DEFAULT_CHARTS_PATH"
)

// CloudManagerConfig extends the base operator configuration with
// cloud-manager-specific settings.
type CloudManagerConfig struct {
	Config `mapstructure:",squash"`

	// RhaiOperatorNamespace is the namespace where the operator is deployed.
	RhaiOperatorNamespace string `mapstructure:"rhai-operator-namespace"`

	// DefaultChartsPath is the base directory for locally-bundled Helm charts.
	DefaultChartsPath string `mapstructure:"default-charts-path"`
}

// BuildCloudManagerConfig builds the cloud manager configuration from viper values.
// It assumes that flags have already been parsed and bound to viper (e.g. by cobra).
func BuildCloudManagerConfig() (*CloudManagerConfig, error) {
	// Ensure the env var bindings exist regardless of whether cobra flags were registered.
	// BindEnv is idempotent — safe to call even if root.go already bound it.
	if err := viper.BindEnv(FlagRhaiOperatorNamespace, EnvRhaiOperatorNamespace); err != nil {
		return nil, fmt.Errorf("failed to bind env var for %s: %w", FlagRhaiOperatorNamespace, err)
	}
	if err := viper.BindEnv(FlagDefaultChartsPath, EnvDefaultChartsPath); err != nil {
		return nil, fmt.Errorf("failed to bind env var for %s: %w", FlagDefaultChartsPath, err)
	}

	var cfg CloudManagerConfig
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cloud manager config: %w", err)
	}

	if err := setupConfig(&cfg.Config); err != nil {
		return nil, err
	}

	if cfg.RhaiOperatorNamespace == "" {
		return nil, fmt.Errorf("operator namespace is required (set via --%s flag or %s env var)", FlagRhaiOperatorNamespace, EnvRhaiOperatorNamespace)
	}

	cfg.DefaultChartsPath = strings.TrimSpace(cfg.DefaultChartsPath)
	if cfg.DefaultChartsPath == "" {
		return nil, fmt.Errorf("default charts path is required (set via --%s flag or %s env var)", FlagDefaultChartsPath, EnvDefaultChartsPath)
	}

	return &cfg, nil
}
