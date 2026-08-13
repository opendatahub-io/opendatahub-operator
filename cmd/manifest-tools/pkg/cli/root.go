package cli

import (
	"github.com/spf13/cobra"
)

type rootOptions struct {
	configFile string
	platform   string
}

func NewRootCommand() *cobra.Command {
	opts := &rootOptions{}

	cmd := &cobra.Command{
		Use:   "manifest-tools",
		Short: "Tools for managing manifest configuration and image overrides",
	}

	cmd.PersistentFlags().StringVar(&opts.configFile, "config", "manifests-config.yaml", "Path to manifests-config.yaml")
	cmd.PersistentFlags().StringVar(&opts.platform, "platform", "odh", "Platform: odh or rhoai (ignored by resolve-digests which processes both)")

	cmd.AddCommand(
		newResolveCommand(opts),
		newApplyOLMCommand(opts),
		newApplyDeployCommand(opts),
		newDownloadCommand(opts),
		newUpdateRefsCommand(opts),
	)

	return cmd
}
