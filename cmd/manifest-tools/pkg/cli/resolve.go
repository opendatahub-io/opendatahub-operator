package cli

import (
	"github.com/spf13/cobra"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/resolver"
)

func newResolveCommand(root *rootOptions) *cobra.Command {
	var manifestsDir string
	var csvImportRegistries []string

	cmd := &cobra.Command{
		Use:   "resolve-digests",
		Short: "Resolve image digests and update manifests-config.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := resolver.Resolve(cmd.Context(), resolver.Options{
				ConfigFile:          root.configFile,
				ManifestsDir:        manifestsDir,
				CSVImportRegistries: csvImportRegistries,
			})
			return err
		},
	}

	cmd.Flags().StringVar(&manifestsDir, "manifests-dir", "opt/manifests", "Downloaded manifests directory")
	cmd.Flags().StringSliceVar(&csvImportRegistries, "csv-import-registries", []string{"quay.io/"}, "Only import CSV images from these registry prefixes (empty = all)")

	return cmd
}
