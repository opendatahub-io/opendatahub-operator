package flags

import (
	"flag"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func AddOperatorFlagsAndEnvvars(envvarPrefix string) error {
	pflag.String("metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	if err := viper.BindEnv("metrics-bind-address", envvarPrefix+"_METRICS_BIND_ADDRESS"); err != nil {
		return err
	}
	pflag.String("health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	if err := viper.BindEnv("health-probe-bind-address", envvarPrefix+"_HEALTH_PROBE_BIND_ADDRESS"); err != nil {
		return err
	}
	pflag.Bool("leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	if err := viper.BindEnv("leader-elect", envvarPrefix+"_LEADER_ELECT"); err != nil {
		return err
	}
	pflag.String("dsc-monitoring-namespace", "opendatahub", "The namespace where data science cluster "+
		"monitoring stack will be deployed")
	if err := viper.BindEnv("dsc-monitoring-namespace", envvarPrefix+"_DSC_MONITORING_NAMESPACE"); err != nil {
		return err
	}
	pflag.String("log-mode", "", "Log mode ('', prod, devel), default to ''")
	if err := viper.BindEnv("log-mode", envvarPrefix+"_LOG_MODE"); err != nil {
		return err
	}
	pflag.String("pprof-bind-address", "", "The address that pprof binds to. ")
	if err := viper.BindEnv("pprof-bind-address", envvarPrefix+"_PPROF_BIND_ADDRESS", "PPROF_BIND_ADDRESS"); err != nil {
		return err
	}

	// zap logging flags
	// these are taken from https://github.com/kubernetes-sigs/controller-runtime/blob/4161b012d114e6c1ea861fd8afcebf7ba2417b49/pkg/log/zap/zap.go#L255
	// and need to be kept in sync.
	// This is needed to enable to configure them through viper and all its supported config sources (i.e. cli flags and env vars)
	pflag.Bool("zap-devel", false,
		"Development Mode defaults(encoder=consoleEncoder,logLevel=Debug,stackTraceLevel=Warn). "+
			"Production Mode defaults(encoder=jsonEncoder,logLevel=Info,stackTraceLevel=Error)")
	if err := viper.BindEnv("zap-devel", "ZAP_DEVEL"); err != nil {
		return err
	}
	pflag.String("zap-encoder", "", "Zap log encoding (one of 'json' or 'console')")
	if err := viper.BindEnv("zap-encoder", "ZAP_ENCODER"); err != nil {
		return err
	}
	pflag.String("zap-log-level", "info",
		"Zap Level to configure the verbosity of logging. Can be one of 'debug', 'info', 'error', "+
			"or any integer value > 0 which corresponds to custom debug levels of increasing verbosity")
	if err := viper.BindEnv("zap-log-level", "ZAP_LOG_LEVEL"); err != nil {
		return err
	}
	pflag.String("zap-stacktrace-level", "",
		"Zap Level at and above which stacktraces are captured (one of 'info', 'error', 'panic').")
	if err := viper.BindEnv("zap-stacktrace-level", "ZAP_STACKTRACE_LEVEL"); err != nil {
		return err
	}
	pflag.String("zap-time-encoding", "", "Zap time encoding (one of 'epoch', 'millis', 'nano', 'iso8601', 'rfc3339' or 'rfc3339nano'). Defaults to 'epoch'.")
	if err := viper.BindEnv("zap-time-encoding", "ZAP_TIME_ENCODING"); err != nil {
		return err
	}

	return nil
}

func NewZapFlagSet() *flag.FlagSet {
	return flag.NewFlagSet(pflag.CommandLine.Name()+" zap configurations", flag.ContinueOnError)
}

func ParseZapFlags(zapFlagSet *flag.FlagSet, zapDevel bool, zapEncoder string, zapLog string, zapStacktrace string, zapTimeEncoding string) error {
	var zapFlagsValues []string
	if zapDevel {
		zapFlagsValues = append(zapFlagsValues, "--zap-devel=true")
	}
	if zapEncoder != "" {
		zapFlagsValues = append(zapFlagsValues, "--zap-encoder="+zapEncoder)
	}
	if zapLog != "" {
		zapFlagsValues = append(zapFlagsValues, "--zap-log-level="+zapLog)
	}
	if zapStacktrace != "" {
		zapFlagsValues = append(zapFlagsValues, "--zap-stacktrace-level="+zapStacktrace)
	}
	if zapTimeEncoding != "" {
		zapFlagsValues = append(zapFlagsValues, "--zap-time-encoding="+zapTimeEncoding)
	}
	return zapFlagSet.Parse(zapFlagsValues)
}
