package cli

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/config"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/runner"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/types"
)

// NewE2ECommand creates the e2e test command
func NewE2ECommand(cfg *config.Config) *cobra.Command {
	var testFilter string
	var testFlags string
	var testPath string
	var workingDir string
	var maxRetries int
	var neverSkip []string
	var skipAtPrefix []string
	var junitOutput string
	var prOpts types.PROptions

	cmd := &cobra.Command{
		Use:   "e2e [-- go-test-args...]",
		Short: "Run e2e tests with retry functionality",
		Long: `Run end-to-end tests with automatic retry for failed tests.
Tests that pass are automatically skipped in subsequent retry attempts using the -skip flag,
making retries more efficient by only re-running tests that are still failing.

Arguments after -- are passed directly to go test command.
Example: test-retry e2e -- -run TestFoo -v`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Combine testFlags with additional args passed after --
			finalTestFlags := testFlags
			if len(args) > 0 {
				if finalTestFlags != "" {
					finalTestFlags += " "
				}
				for _, arg := range args {
					finalTestFlags += arg + " "
				}
				finalTestFlags = finalTestFlags[:len(finalTestFlags)-1] // Remove trailing space
			}

			opts := types.E2ETestOptions{
				Config:            cfg,
				MaxRetries:        maxRetries,
				TestFilter:        testFilter,
				TestFlags:         finalTestFlags,
				TestPath:          testPath,
				WorkingDir:        workingDir,
				NeverSkipPrefixes: neverSkip,
				SkipAtPrefixes:    skipAtPrefix,
				PROptions:         prOpts,
				JUnitOutputPath:   junitOutput,
			}

			testRunner := runner.NewE2ETestRunner(opts)
			return testRunner.Run()
		},
	}

	cmd.Flags().StringVar(&testFilter, "filter", "^TestOdhOperator/", "Filter tests to run (regex pattern)")
	cmd.Flags().StringVar(&testPath, "path", "./tests/e2e/", "Path to e2e tests")
	cmd.Flags().StringVar(&workingDir, "working-dir", "", "Working directory for running go test (default: current directory)")
	cmd.Flags().IntVar(&maxRetries, "max-retries", 3, "Maximum number of retries for failed tests")
	cmd.Flags().StringSliceVar(&neverSkip, "never-skip", []string{"TestOdhOperator/DSCInitialization_and_DataScienceCluster_management_E2E_Tests", "TestOdhOperator/DataScienceCluster"}, "Test prefixes that should never be skipped (always run, repeatable)")
	cmd.Flags().StringSliceVar(&skipAtPrefix, "skip-at-prefix", []string{"TestOdhOperator/services/*/", "TestOdhOperator/components/*/", "TestOdhOperator/"}, "Test prefixes where tests should be extracted at prefix + 1 level (repeatable)")
	cmd.Flags().StringVar(&junitOutput, "junit-output", "", "Path to JUnit XML output file (optional)")

	// GitHub PR notification flags
	cmd.Flags().StringVar(&prOpts.Token, "github-token", "", "GitHub token for authentication (can also use GITHUB_TOKEN env var)")
	cmd.Flags().StringVar(&prOpts.Owner, "github-owner", "", "GitHub repository owner")
	cmd.Flags().StringVar(&prOpts.Repo, "github-repo", "", "GitHub repository name")
	cmd.Flags().IntVar(&prOpts.PRNumber, "github-pr", 0, "GitHub pull request number to notify on test failures")
	cmd.Flags().StringVar(&prOpts.Label, "failure-label", "", "Label to add to PR when tests fail (optional)")
	cmd.Flags().StringVar(&prOpts.Comment, "failure-comment", "", "Comment to add to PR when tests fail (optional)")

	// Bind the github-token flag to viper and set up env var binding
	viper.BindPFlag("github-token", cmd.Flags().Lookup("github-token"))
	viper.BindEnv("github-token", "GITHUB_TOKEN")

	return cmd
}
