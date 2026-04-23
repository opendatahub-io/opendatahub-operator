package azure

import (
	"github.com/spf13/cobra"

	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/azure/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/cloudmanager/app"
	azurectrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/azure"
)

// NewCmd returns the cobra command for the Azure cloud manager.
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "azure",
		Short: "Run the Azure cloud manager",
		Long:  "Start the cloud manager operator for Azure Kubernetes Engine (AKS) clusters.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return app.Run(cmd, app.Provider{
				Name:             "azure",
				AddToScheme:      ccmv1alpha1.AddToScheme,
				LeaderElectionID: "azure.cloudmanager.opendatahub.io",
				NewReconciler:    azurectrl.NewReconciler,
			})
		},
	}

	return cmd
}
