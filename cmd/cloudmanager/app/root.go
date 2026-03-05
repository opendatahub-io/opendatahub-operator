package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const envPrefix = "CLOUD_MANAGER"

var rootCmd = &cobra.Command{
	Use:   "cloudmanager",
	Short: "Cloud manager operator for OpenDataHub",
	Long:  "A unified cloud manager operator that manages cloud-provider-specific Kubernetes clusters for OpenDataHub.",
	PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
		viper.SetEnvPrefix(envPrefix)
		viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
		viper.AutomaticEnv()

		return viper.BindPFlags(cmd.Flags())
	},
}

func init() { //nolint:gochecknoinits
	addCommonFlags()
}

// AddCommand adds a subcommand to the root command.
func AddCommand(cmd *cobra.Command) {
	rootCmd.AddCommand(cmd)
}

func addCommonFlags() {
	pf := rootCmd.PersistentFlags()

	pf.String("metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	pf.String("health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	pf.Bool("leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	pf.String("log-mode", "", "Log mode ('', prod, devel), default to ''")

	// zap logging flags
	pf.Bool("zap-devel", false,
		"Development Mode defaults(encoder=consoleEncoder,logLevel=Debug,stackTraceLevel=Warn). "+
			"Production Mode defaults(encoder=jsonEncoder,logLevel=Info,stackTraceLevel=Error)")
	pf.String("zap-encoder", "", "Zap log encoding (one of 'json' or 'console')")
	pf.String("zap-log-level", "info",
		"Zap Level to configure the verbosity of logging. Can be one of 'debug', 'info', 'error', "+
			"or any integer value > 0 which corresponds to custom debug levels of increasing verbosity")
	pf.String("zap-stacktrace-level", "",
		"Zap Level at and above which stacktraces are captured (one of 'info', 'error', 'panic').")
	pf.String("zap-time-encoding", "", "Zap time encoding (one of 'epoch', 'millis', 'nano', 'iso8601', 'rfc3339' or 'rfc3339nano'). Defaults to 'epoch'.")

	// Bind env vars explicitly to match the pattern from pkg/utils/flags/flags.go
	must(viper.BindEnv("metrics-bind-address", envPrefix+"_METRICS_BIND_ADDRESS"))
	must(viper.BindEnv("health-probe-bind-address", envPrefix+"_HEALTH_PROBE_BIND_ADDRESS"))
	must(viper.BindEnv("leader-elect", envPrefix+"_LEADER_ELECT"))
	must(viper.BindEnv("log-mode", envPrefix+"_LOG_MODE"))
	must(viper.BindEnv("zap-devel", "ZAP_DEVEL"))
	must(viper.BindEnv("zap-encoder", "ZAP_ENCODER"))
	must(viper.BindEnv("zap-log-level", "ZAP_LOG_LEVEL"))
	must(viper.BindEnv("zap-stacktrace-level", "ZAP_STACKTRACE_LEVEL"))
	must(viper.BindEnv("zap-time-encoding", "ZAP_TIME_ENCODING"))
}

func must(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error binding env var: %v\n", err)
		os.Exit(1)
	}
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
