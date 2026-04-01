package operatorconfig

import (
	"fmt"

	"github.com/spf13/viper"
)

const (
	// FlagRhaiOperatorNamespace is the viper/flag key for the operator namespace.
	FlagRhaiOperatorNamespace = "rhai-operator-namespace"
	// EnvRhaiOperatorNamespace is the environment variable name for the operator namespace.
	EnvRhaiOperatorNamespace = "RHAI_OPERATOR_NAMESPACE"
)

// CloudManagerConfig extends the base operator configuration with
// cloud-manager-specific settings.
type CloudManagerConfig struct {
	Config `mapstructure:",squash"`

	// RhaiOperatorNamespace is the namespace where the operator is deployed.
	RhaiOperatorNamespace string `mapstructure:"rhai-operator-namespace"`
}

// BuildCloudManagerConfig builds the cloud manager configuration from viper values.
// It assumes that flags have already been parsed and bound to viper (e.g. by cobra).
func BuildCloudManagerConfig() (*CloudManagerConfig, error) {
	// Ensure the env var binding exists regardless of whether cobra flags were registered.
	// BindEnv is idempotent — safe to call even if root.go already bound it.
	if err := viper.BindEnv(FlagRhaiOperatorNamespace, EnvRhaiOperatorNamespace); err != nil {
		return nil, fmt.Errorf("failed to bind env var for %s: %w", FlagRhaiOperatorNamespace, err)
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

	return &cfg, nil
}
