package logger

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/go-logr/logr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	ctrlzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var currentLogLevel atomic.Value

// copy from controller-runtime/pkg/log/zap/flag.go.
var levelStrings = map[string]zapcore.Level{
	"debug": zap.DebugLevel,
	"info":  zap.InfoLevel,
	"error": zap.ErrorLevel,
}

// adjusted copy from controller-runtime/pkg/log/zap/flag.go, keep the same argument name.
func stringToLevel(flagValue string) (zapcore.Level, error) {
	level, validLevel := levelStrings[strings.ToLower(flagValue)]
	if validLevel {
		return level, nil
	}
	logLevel, err := strconv.ParseInt(flagValue, 10, 8)
	if err != nil {
		return 0, fmt.Errorf("invalid log level \"%s\"", flagValue)
	}
	if logLevel > 0 {
		intLevel := -1 * int8(logLevel)
		return zapcore.Level(intLevel), nil
	}

	return 0, fmt.Errorf("invalid log level \"%s\"", flagValue)
}

func SetLevel(levelStr string) error {
	if levelStr == "" {
		return nil
	}
	levelNum, err := stringToLevel(levelStr)
	if err != nil {
		return err
	}

	// We must be sure that ctrlzap.Options.Level is not nil when addDefaults() is called
	// because otherwise ctrlzap.addDefaults() will set the log level to an AtomicLevel we don't have a reference to
	level, ok := currentLogLevel.Load().(zap.AtomicLevel)
	if !ok {
		return errors.New("stored loglevel is not of type *zap.AtomicLevel")
	}

	level.SetLevel(levelNum)
	return nil
}

func NewLogger(mode string, override *ctrlzap.Options) logr.Logger {
	opts := newBaseOptionsFromMode(mode)
	overrideOptions(opts, override)
	currentLogLevel.Store(opts.Level)
	return ctrlzap.New(ctrlzap.UseFlagOptions(opts))
}

func newBaseOptionsFromMode(mode string) *ctrlzap.Options {
	var opts ctrlzap.Options

	switch mode {
	case "devel", "development": //  the most logging verbosity
		opts = ctrlzap.Options{
			Development:     true,
			StacktraceLevel: zapcore.WarnLevel,
			DestWriter:      os.Stdout,
		}
	case "prod", "production": // the least logging verbosity
		opts = ctrlzap.Options{
			Development:     false,
			StacktraceLevel: zapcore.ErrorLevel,
			DestWriter:      os.Stdout,
			EncoderConfigOptions: []ctrlzap.EncoderConfigOption{func(config *zapcore.EncoderConfig) {
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
		opts = ctrlzap.Options{
			Development:     false,
			StacktraceLevel: zapcore.ErrorLevel,
			DestWriter:      os.Stdout,
		}
	}

	return &opts
}

func overrideOptions(orig, override *ctrlzap.Options) {
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
