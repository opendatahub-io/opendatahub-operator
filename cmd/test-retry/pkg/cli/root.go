package cli

import (
	"github.com/spf13/cobra"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/config"
)

// NewRootCommand creates the root command for test-retry CLI
func NewRootCommand() *cobra.Command {
	cfg := &config.Config{}

	rootCmd := &cobra.Command{
		Use:   "test-retry",
		Short: "A CLI tool for running Go tests with automatic retry functionality",
		Long: `test-retry is a CLI tool that runs Go tests with intelligent retry logic.
It tracks which tests pass and skips them in subsequent retries using the -skip flag,
while continuing to retry only the tests that are still failing. This makes test
execution more efficient and reliable.`,
	}

	// Add persistent flags
	rootCmd.PersistentFlags().BoolVar(&cfg.Verbose, "verbose", false, "Enable verbose output")

	// Add subcommands
	rootCmd.AddCommand(NewE2ECommand(cfg))

	return rootCmd
}
