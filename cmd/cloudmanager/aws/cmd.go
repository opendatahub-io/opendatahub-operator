package aws

import (
	"github.com/spf13/cobra"

	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/aws/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/cloudmanager/app"
	awsctrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/aws"
)

// NewCmd returns the cobra command for the AWS cloud manager.
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "aws",
		Short: "Run the AWS cloud manager",
		Long:  "Start the cloud manager operator for AWS Kubernetes Service clusters.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return app.Run(cmd, app.Provider{
				Name:             "aws",
				AddToScheme:      ccmv1alpha1.AddToScheme,
				LeaderElectionID: "aws.cloudmanager.opendatahub.io",
				NewReconciler:    awsctrl.NewReconciler,
			})
		},
	}

	return cmd
}
