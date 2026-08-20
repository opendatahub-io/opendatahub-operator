package cli

import (
	"github.com/spf13/cobra"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/applier"
)

func newApplyDeployCommand(root *rootOptions) *cobra.Command {
	var managerFile string

	cmd := &cobra.Command{
		Use:   "apply-deploy",
		Short: "Apply image overrides to manager.yaml for make deploy",
		RunE: func(cmd *cobra.Command, args []string) error {
			return applier.ApplyDeploy(applier.DeployOptions{
				ConfigFile:  root.configFile,
				Platform:    root.platform,
				ManagerFile: managerFile,
			})
		},
	}

	cmd.Flags().StringVar(&managerFile, "manager-file", "config/manager/manager.yaml", "Path to manager.yaml")

	return cmd
}
