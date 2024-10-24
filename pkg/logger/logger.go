package logger

import (
	"os"
	"strings"

	"github.com/go-logr/logr"
	"go.uber.org/zap/zapcore"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var logLevelMapping = map[string]int{
	"devel":   0,
	"default": 1, // default one when not set log-mode
	"prod":    2,
}

// in each controller, to use different log level.
func LogWithLevel(logger logr.Logger, level string) logr.Logger {
	level = strings.TrimSpace(level)
	verbosityLevel, ok := logLevelMapping[level]
	if !ok {
		verbosityLevel = 1 // fallback to info level
	}
	return logger.V(verbosityLevel)
}

// in DSC component, to use different mode for logging, e.g. development, production
// when not set mode it falls to "default" which is used by startup main.go.
func ConfigLoggers(mode string) logr.Logger {
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
	return zap.New(zap.UseFlagOptions(&opts))
}
