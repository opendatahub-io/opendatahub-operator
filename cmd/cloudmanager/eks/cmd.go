package eks

import (
	"github.com/spf13/cobra"

	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/eks/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/cloudmanager/app"
	eksctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/eks"
)

// NewCmd returns the cobra command for the EKS cloud manager.
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "eks",
		Short: "Run the EKS cloud manager",
		Long:  "Start the cloud manager operator for Amazon Elastic Kubernetes Service (EKS) clusters.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return app.Run(cmd, app.Provider{
				Name:             "eks",
				AddToScheme:      ccmv1alpha1.AddToScheme,
				LeaderElectionID: "eks.cloudmanager.opendatahub.io",
				NewReconciler:    eksctrl.NewReconciler,
			})
		},
	}

	return cmd
}
