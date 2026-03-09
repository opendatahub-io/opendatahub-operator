package flags

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// RegisterComponentSuppressionFlags registers suppression flags for a list of component names.
func RegisterComponentSuppressionFlags(names []string) error {
	for _, name := range names {
		if err := registerComponentSuppressionFlag(name); err != nil {
			return err
		}
	}
	return nil
}

// RegisterServiceSuppressionFlags registers suppression flags for a list of service names.
func RegisterServiceSuppressionFlags(names []string) error {
	for _, name := range names {
		if err := registerServiceSuppressionFlag(name); err != nil {
			return err
		}
	}
	return nil
}

// IsDSCEnabled returns true if the DSC resource is enabled.
func IsDSCEnabled() bool {
	return !viper.GetBool("disable-dsc-resource")
}

// IsDSCIEnabled returns true if the DSCI resource is enabled.
func IsDSCIEnabled() bool {
	return !viper.GetBool("disable-dsci-resource")
}

// IsComponentEnabled returns true if the named component is enabled.
func IsComponentEnabled(name string) bool {
	return !viper.GetBool(fmt.Sprintf("disable-%s-component", name))
}

// IsServiceEnabled returns true if the named service is enabled.
func IsServiceEnabled(name string) bool {
	return !viper.GetBool(fmt.Sprintf("disable-%s-service", name))
}

// AddResourceSuppressionFlags registers the DSC and DSCI resource suppression flags.
// Each flag is bound to a corresponding RHAI_ environment variable.
func addResourceSuppressionFlags() error {
	pflag.Bool("disable-dsc-resource", false, "Suppress the DSC controller, webhooks, and auto-creation")
	if err := viper.BindEnv("disable-dsc-resource", "RHAI_DISABLE_DSC_RESOURCE"); err != nil {
		return err
	}

	pflag.Bool("disable-dsci-resource", false, "Suppress the DSCI controller, webhooks, and auto-creation")
	if err := viper.BindEnv("disable-dsci-resource", "RHAI_DISABLE_DSCI_RESOURCE"); err != nil {
		return err
	}

	return nil
}

// RegisterComponentSuppressionFlag registers a suppression flag for a component.
// The flag name is "disable-{name}-component" and it is bound to the env var RHAI_DISABLE_{UPPER(name)}_COMPONENT.
func registerComponentSuppressionFlag(name string) error {
	flagName := fmt.Sprintf("disable-%s-component", name)
	envVar := fmt.Sprintf("RHAI_DISABLE_%s_COMPONENT", strings.ToUpper(name))

	pflag.Bool(flagName, false, fmt.Sprintf("Suppress the %s component reconciler and related webhooks", name))
	if err := viper.BindEnv(flagName, envVar); err != nil {
		return err
	}

	return nil
}

// RegisterServiceSuppressionFlag registers a suppression flag for a service.
// The flag name is "disable-{name}-service" and it is bound to the env var RHAI_DISABLE_{UPPER(name)}_SERVICE.
func registerServiceSuppressionFlag(name string) error {
	flagName := fmt.Sprintf("disable-%s-service", name)
	envVar := fmt.Sprintf("RHAI_DISABLE_%s_SERVICE", strings.ToUpper(name))

	pflag.Bool(flagName, false, fmt.Sprintf("Suppress the %s service reconciler", name))
	if err := viper.BindEnv(flagName, envVar); err != nil {
		return err
	}

	return nil
}
