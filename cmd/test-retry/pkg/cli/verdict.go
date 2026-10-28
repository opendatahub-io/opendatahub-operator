package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/test-retry/pkg/verdict"
)

// ExitError preserves a policy-selected process exit code through Cobra.
type ExitError struct {
	Code int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("exit status %d", e.Code)
}

// NewVerdictCommand creates the policy-aware E2E verdict command.
func NewVerdictCommand() *cobra.Command {
	var coreExit int
	var dagExit int
	var dagReportPath string
	var outputPath string
	var coreOnly bool

	cmd := &cobra.Command{
		Use:           "verdict",
		Short:         "Evaluate core and production DAG E2E results",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			result, exitCode := verdict.Evaluate(coreExit, dagExit, verdict.ReadReport(dagReportPath), coreOnly)
			verdict.Print(cmd.OutOrStdout(), result)
			if outputPath != "" {
				if err := verdict.WriteResult(outputPath, result); err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "write E2E summary: %v\n", err)
					if exitCode == 0 {
						exitCode = 1
					}
				}
			}
			if exitCode != 0 {
				return &ExitError{Code: exitCode}
			}
			return nil
		},
	}

	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)
	cmd.Flags().IntVar(&coreExit, "core-exit", 0, "core E2E process exit code")
	cmd.Flags().IntVar(&dagExit, "dag-exit", -1, "DAG E2E process exit code; -1 means not run")
	cmd.Flags().StringVar(&dagReportPath, "dag-report", "", "optional attributed DAG report JSON")
	cmd.Flags().StringVar(&outputPath, "output", "", "optional summary JSON output path")
	cmd.Flags().BoolVar(&coreOnly, "core-only", false, "evaluate only core and synthetic tests")
	return cmd
}
