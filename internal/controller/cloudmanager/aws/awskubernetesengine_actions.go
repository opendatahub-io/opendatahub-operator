package aws

import (
	"context"
	"fmt"

	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/aws/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// initialize populates the HelmCharts list based on the AWSKubernetesEngine spec.
// This action runs first and sets up what charts need to be rendered.
func initialize(ctx context.Context, rr *types.ReconciliationRequest) error {
	instance, ok := rr.Instance.(*ccmv1alpha1.AWSKubernetesEngine)
	if !ok {
		return fmt.Errorf("resource instance is not an AWSKubernetesEngine (got %T)", rr.Instance)
	}

	rr.HelmCharts = common.BuildHelmCharts(instance.Spec.Dependencies, rr.ChartsBasePath)
	return nil
}
