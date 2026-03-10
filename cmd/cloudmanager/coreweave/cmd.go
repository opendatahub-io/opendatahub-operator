package coreweave

import (
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"

	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/coreweave/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/cmd/cloudmanager/app"
	coreweavectrl "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/coreweave"
)

// NewCmd returns the cobra command for the CoreWeave cloud manager.
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "coreweave",
		Short: "Run the CoreWeave cloud manager",
		Long:  "Start the cloud manager operator for CoreWeave Kubernetes Engine clusters.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return app.Run(cmd, app.Provider{
				Name:             "coreweave",
				AddToScheme:      ccmv1alpha1.AddToScheme,
				LeaderElectionID: "coreweave.cloudmanager.opendatahub.io",
				NewReconciler:    coreweavectrl.NewReconciler,
				CacheOptions:     cacheOptions,
			})
		},
	}

	return cmd
}

func cacheOptions(scheme *runtime.Scheme) (cache.Options, error) {
	kind := ccmv1alpha1.CoreWeaveKubernetesEngineKind
	return app.DefaultCacheOptions(scheme, kind)
}
