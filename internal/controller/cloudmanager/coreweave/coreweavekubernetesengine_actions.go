package coreweave

import (
	"context"
	"fmt"

	ccmv1alpha1 "github.com/opendatahub-io/opendatahub-operator/v2/api/cloudmanager/coreweave/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/cloudmanager/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// initialize populates the HelmCharts list based on the CoreWeaveKubernetesEngine spec.
// This action runs first and sets up what charts need to be rendered.
func initialize(ctx context.Context, rr *types.ReconciliationRequest) error {
	instance, ok := rr.Instance.(*ccmv1alpha1.CoreWeaveKubernetesEngine)
	if !ok {
		return fmt.Errorf("resource instance is not a CoreWeaveKubernetesEngine (got %T)", rr.Instance)
	}

	rr.HelmCharts = common.BuildHelmCharts(instance.Spec.Dependencies)
	return nil
}
