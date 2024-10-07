package logger

import (
	"flag"
	"os"

	"github.com/go-logr/logr"
	"go.uber.org/zap/zapcore"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// NewNamedLogger creates a new logger for a component.
// If the mode is set (so can be different from the default one),
// it will create a new logger with the specified mode's options.
func NewNamedLogger(log logr.Logger, name string, mode string) logr.Logger {
	if mode != "" {
		log = NewLogger(mode)
	}
	return log.WithName(name)
}

func NewLoggerWithOptions(mode string, override *zap.Options) logr.Logger {
	opts := newOptions(mode)
	overrideOptions(opts, override)
	return newLogger(opts)
}

// in DSC component, to use different mode for logging, e.g. development, production
// when not set mode it falls to "default" which is used by startup main.go.
func NewLogger(mode string) logr.Logger {
	return newLogger(newOptions(mode))
}

func newLogger(opts *zap.Options) logr.Logger {
	return zap.New(zap.UseFlagOptions(opts))
}

func newOptions(mode string) *zap.Options {
	var opts zap.Options
	switch mode {
	case "devel", "development": //  the most logging verbosity
		opts = zap.Options{
			Development:     true,
			StacktraceLevel: zapcore.WarnLevel,
			Level:           zapcore.InfoLevel,
			DestWriter:      os.Stdout,
		}
	case "prod", "production": // the least logging verbosity
		opts = zap.Options{
			Development:     false,
			StacktraceLevel: zapcore.ErrorLevel,
			Level:           zapcore.InfoLevel,
			DestWriter:      os.Stdout,
			EncoderConfigOptions: []zap.EncoderConfigOption{func(config *zapcore.EncoderConfig) {
				config.EncodeTime = zapcore.ISO8601TimeEncoder // human readable not epoch
				config.EncodeDuration = zapcore.SecondsDurationEncoder
				config.LevelKey = "LogLevel"
				config.NameKey = "Log"
				config.CallerKey = "Caller"
				config.MessageKey = "Message"
				config.TimeKey = "Time"
				config.StacktraceKey = "Stacktrace"
			}},
		}
	default:
		opts = zap.Options{
			Development:     false,
			StacktraceLevel: zapcore.ErrorLevel,
			Level:           zapcore.InfoLevel,
			DestWriter:      os.Stdout,
		}
	}
	return &opts
}

func overrideOptions(orig, override *zap.Options) {
	// Development is boolean, cannot check for nil, so check if it was set
	isDevelopmentSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "zap-devel" {
			isDevelopmentSet = true
		}
	})
	if isDevelopmentSet {
		orig.Development = override.Development
	}

	if override.StacktraceLevel != nil {
		orig.StacktraceLevel = override.StacktraceLevel
	}

	if override.Level != nil {
		orig.Level = override.Level
	}

	if override.DestWriter != nil {
		orig.DestWriter = override.DestWriter
	}

	if override.EncoderConfigOptions != nil {
		orig.EncoderConfigOptions = override.EncoderConfigOptions
	}
}
