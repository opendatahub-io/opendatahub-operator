package ogx

import (
	"context"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/precondition"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

func initialize(_ context.Context, rr *odhtypes.ReconciliationRequest) error { //nolint:unparam
	rr.Manifests = append(rr.Manifests, manifestPath(rr.ManifestsBasePath, rr.Release.Name))
	return nil
}

func checkPreConditions(ctx context.Context, rr *odhtypes.ReconciliationRequest) (precondition.CheckResult, error) {
	dsc, err := cluster.GetDSC(ctx, rr.Client)
	if err != nil {
		return precondition.CheckResult{}, fmt.Errorf("failed to get DataScienceCluster: %w", err)
	}

	if dsc.Spec.Components.LlamaStackOperator.ManagementState == operatorv1.Managed {
		return precondition.CheckResult{
			Pass:    false,
			Message: fmt.Sprintf("LlamaStackOperator is set to %s, it has been deprecated, please set it to %s before enabling OGX", operatorv1.Managed, operatorv1.Removed),
		}, nil
	}

	return precondition.CheckResult{Pass: true}, nil
}
