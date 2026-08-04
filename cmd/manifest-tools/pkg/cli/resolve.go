package cli

import (
	"github.com/spf13/cobra"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/resolver"
)

func newResolveCommand(root *rootOptions) *cobra.Command {
	var buildConfigRepo string
	var buildConfigBranch string
	var manifestsDir string

	cmd := &cobra.Command{
		Use:   "resolve-digests",
		Short: "Resolve image digests from Build-Config and update manifests-config.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := resolver.Resolve(resolver.Options{
				ConfigFile:        root.configFile,
				ManifestsDir:      manifestsDir,
				BuildConfigRepo:   buildConfigRepo,
				BuildConfigBranch: buildConfigBranch,
			})
			return err
		},
	}

	cmd.Flags().StringVar(&buildConfigRepo, "build-config-repo", "opendatahub-io/ODH-Build-Config", "Build-Config repository (org/repo)")
	cmd.Flags().StringVar(&buildConfigBranch, "build-config-branch", "main", "Build-Config branch")
	cmd.Flags().StringVar(&manifestsDir, "manifests-dir", "opt/manifests", "Downloaded manifests directory")

	return cmd
}
