package cli

import (
	"github.com/spf13/cobra"

	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/manifest-tools/pkg/applier"
)

func newApplyOLMCommand(root *rootOptions) *cobra.Command {
	var namespace string
	var operatorPackage string
	var extraEnv []string

	cmd := &cobra.Command{
		Use:   "apply-olm",
		Short: "Apply image overrides to OLM Subscription from manifests-config.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			return applier.ApplyOLM(applier.Options{
				ConfigFile:      root.configFile,
				Platform:        root.platform,
				Namespace:       namespace,
				OperatorPackage: operatorPackage,
				ExtraEnv:        extraEnv,
			})
		},
	}

	cmd.Flags().StringVar(&namespace, "namespace", "opendatahub-operator", "Operator namespace")
	cmd.Flags().StringVar(&operatorPackage, "package", "opendatahub-operator", "Operator package name")
	cmd.Flags().StringArrayVar(&extraEnv, "extra-env", nil, "Additional operator env var to inject into the Subscription, on top of image overrides (repeatable, KEY=VALUE)")

	return cmd
}
