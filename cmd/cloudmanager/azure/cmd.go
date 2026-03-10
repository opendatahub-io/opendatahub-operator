package azure

import (
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"

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
				CacheOptions:     cacheOptions,
			})
		},
	}

	return cmd
}

func cacheOptions(scheme *runtime.Scheme) (cache.Options, error) {
	kind := ccmv1alpha1.AzureKubernetesEngineKind
	return app.DefaultCacheOptions(scheme, kind)
}
