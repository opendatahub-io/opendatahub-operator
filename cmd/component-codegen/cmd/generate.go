package cmd

import (
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/component-codegen/cmd/generator"
)

func newLogger() *logrus.Logger {
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)
	return logger
}

var generateCmd = &cobra.Command{
	Use:   "generate [component-name]",
	Short: "Generates boilerplate folders/files for a new component",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		logger := newLogger()
		componentName := args[0]
		if err := generator.GenerateComponent(logger, componentName); err != nil {
			logger.Errorf("Failed to generate component: %v", err)
		}
	},
}

func init() { //nolint:gochecknoinits
	rootCmd.AddCommand(generateCmd)
}
