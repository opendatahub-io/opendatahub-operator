package cli

import (
	"github.com/spf13/cobra"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/generator"
)

func newGenerateCommand(root *rootOptions) *cobra.Command {
	var outputFile string

	cmd := &cobra.Command{
		Use:   "generate-overrides",
		Short: "Generate RELATED_IMAGE_* override env file from manifests-config.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			return generator.Generate(generator.Options{
				ConfigFile: root.configFile,
				Platform:   root.platform,
				OutputFile: outputFile,
			})
		},
	}

	cmd.Flags().StringVar(&outputFile, "output", "opt/related-images-override.env", "Output env file path")

	return cmd
}
