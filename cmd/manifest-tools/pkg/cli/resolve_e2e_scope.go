package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/opendatahub-io/opendatahub-operator/pkg/scoperules"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/e2escope"
)

// newResolveE2EScopeCommand prints which e2e components and services a
// change affects, so the Makefile's e2e-test target can narrow
// E2E_TEST_COMPONENT/E2E_TEST_SERVICE instead of running everything.
//
// On success it always prints both lines, space-separated:
//
//	COMPONENTS=foo bar
//	SERVICES=baz
//
// An empty line means "confirmed zero", not "not checked" — the caller
// should skip that dimension entirely. A non-zero exit with no output
// means the change can't be scoped safely, so the caller must run the
// full suite.
func newResolveE2EScopeCommand(root *rootOptions) *cobra.Command {
	var rulesPath string

	cmd := &cobra.Command{
		Use:   "resolve-e2e-scope",
		Short: "Print which e2e components/services the current change affects",
		Long: "Print which e2e components/services the current change affects.\n\n" +
			"The repository root is derived from --config's own directory, so --config must point at the real manifests-config.yaml at the repository root, not a copy elsewhere.",
		// Falling back to the full suite (a non-zero exit) is this command's
		// designed, frequent outcome -- not a flag-usage mistake -- so its
		// usage block must not clutter the SELECTIVE-E2E: diagnostic log.
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			repoRoot := filepath.Dir(root.configFile)
			if _, err := os.Stat(filepath.Join(repoRoot, ".git")); err != nil {
				return fmt.Errorf("--config %q is not at the repository root (no .git found in %q) -- "+
					"point --config at the real manifests-config.yaml, not a copy elsewhere", root.configFile, repoRoot)
			}
			rules := rulesPath
			if rules == "" {
				rules = filepath.Join(repoRoot, scoperules.DefaultPath)
			}
			return runResolveE2EScope(cmd, repoRoot, rules, root.configFile, cmd.OutOrStdout())
		},
	}

	cmd.Flags().StringVar(&rulesPath, "rules", "", "path to e2e-scope-rules.yaml (default: <repo root>/"+scoperules.DefaultPath+")")

	return cmd
}

func runResolveE2EScope(cmd *cobra.Command, repoRoot, rulesPath, configFile string, stdout io.Writer) error {
	result, err := e2escope.Resolve(cmd.Context(), e2escope.Options{
		RepoRoot:   repoRoot,
		RulesPath:  rulesPath,
		ConfigFile: configFile,
	})
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(stdout, "COMPONENTS=%s\n", strings.Join(result.Components, " ")); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "SERVICES=%s\n", strings.Join(result.Services, " "))
	return err
}
