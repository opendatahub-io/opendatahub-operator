package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/downloader"
)

func newDownloadCommand(root *rootOptions) *cobra.Command {
	var manifestsDir string
	var chartsDir string
	var useLocal bool
	var componentOverrides []string

	cmd := &cobra.Command{
		Use:   "download",
		Short: "Download component manifests and charts from remote git repos",
		Long: `Reads manifests-config.yaml and downloads component manifests into opt/manifests/
and charts into opt/charts/ using shallow git clones. Supports branch@sha tracking
format, tag/branch refs, and local checkout fallback.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			platform := root.platform
			if platform == "" {
				platform = os.Getenv("ODH_PLATFORM_TYPE")
			}

			ul := useLocal
			if !ul && os.Getenv("USE_LOCAL") == "true" {
				ul = true
			}

			overrides, err := parseOverrides(append(componentOverrides, args...))
			if err != nil {
				return err
			}

			return downloader.Download(cmd.Context(), downloader.Options{
				ConfigFile:   root.configFile,
				Platform:     platform,
				ManifestsDir: manifestsDir,
				ChartsDir:    chartsDir,
				UseLocal:     ul,
				Overrides:    overrides,
			})
		},
	}

	cmd.Flags().StringVar(&manifestsDir, "manifests-dir", "opt/manifests", "Destination directory for component manifests")
	cmd.Flags().StringVar(&chartsDir, "charts-dir", "opt/charts", "Destination directory for charts")
	cmd.Flags().BoolVar(&useLocal, "use-local", false, "Copy from adjacent local checkout instead of cloning")
	cmd.Flags().StringArrayVar(&componentOverrides, "component", nil, "Override a component source: key=org:repo:ref:sourcePath")

	cmd.Args = func(cmd *cobra.Command, args []string) error {
		for _, a := range args {
			parts := splitFirst(a, '=')
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return fmt.Errorf("positional arg %q must be in format key=org:repo:ref:sourcePath", a)
			}
		}
		return nil
	}

	return cmd
}

func parseOverrides(raw []string) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	result := make(map[string]string, len(raw))
	for _, s := range raw {
		parts := splitFirst(s, '=')
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, &overrideError{value: s}
		}
		result[parts[0]] = parts[1]
	}
	return result, nil
}

func splitFirst(s string, sep byte) []string {
	for i := range s {
		if s[i] == sep {
			return []string{s[:i], s[i+1:]}
		}
	}
	return []string{s}
}

type overrideError struct {
	value string
}

func (e *overrideError) Error() string {
	return "invalid override format " + e.value + "; expected key=org:repo:ref:sourcePath"
}
