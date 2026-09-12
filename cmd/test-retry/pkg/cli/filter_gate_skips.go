package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/formatter"
)

func NewFilterGateSkipsCommand() *cobra.Command {
	var junitPath string

	cmd := &cobra.Command{
		Use:   "filter-gate-skips",
		Short: "Remove tag-gate skipped testcases from a JUnit XML report",
		Long: `Filter gate-skipped testcases from a gotestsum/test-retry JUnit XML report.

Only removes skips whose message contains "Skipping test: passed tag:".

The file is overwritten in place. Real skips, passes, and failures are kept.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if junitPath == "" {
				return fmt.Errorf("--junit is required")
			}
			if err := formatter.FilterGateSkippedTestsFile(junitPath); err != nil {
				return err
			}
			fmt.Printf("Filtered gate-skipped testcases from %s\n", junitPath)
			return nil
		},
	}

	cmd.Flags().StringVarP(&junitPath, "junit", "j", "", "Path to the JUnit XML report")
	_ = cmd.MarkFlagRequired("junit")
	return cmd
}