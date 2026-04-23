package azure

import (
	"context"
	"fmt"

	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/azure/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// newInitializeAction returns an action that populates the HelmCharts list based on the
// AzureKubernetesEngine spec. It runs first and sets up what charts need to be rendered.
func newInitializeAction(chartsPath string) actions.Fn {
	return func(ctx context.Context, rr *types.ReconciliationRequest) error {
		instance, ok := rr.Instance.(*ccmv1alpha1.AzureKubernetesEngine)
		if !ok {
			return fmt.Errorf("resource instance is not an AzureKubernetesEngine (got %T)", rr.Instance)
		}

		rr.HelmCharts = common.BuildHelmCharts(chartsPath, instance.Spec.Dependencies)
		return nil
	}
}
