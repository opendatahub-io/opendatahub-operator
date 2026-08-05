package cli

import (
	"github.com/spf13/cobra"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/resolver"
)

func newResolveCommand(root *rootOptions) *cobra.Command {
	var manifestsDir string

	cmd := &cobra.Command{
		Use:   "resolve-digests",
		Short: "Resolve image digests and update manifests-config.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := resolver.Resolve(cmd.Context(), resolver.Options{
				ConfigFile:   root.configFile,
				ManifestsDir: manifestsDir,
			})
			return err
		},
	}

	cmd.Flags().StringVar(&manifestsDir, "manifests-dir", "opt/manifests", "Downloaded manifests directory")

	return cmd
}
