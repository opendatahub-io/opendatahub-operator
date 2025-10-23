package actions

import (
	"context"
	"fmt"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhTypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

func ApplicationNamespace(ctx context.Context, rr *odhTypes.ReconciliationRequest) (string, error) {
	if rr.DSCI != nil {
		return rr.DSCI.Spec.ApplicationsNamespace, nil
	}

	// DSCI was not available during reconciliation setup (e.g., service started before
	// platform initialization). Attempt an on-demand fetch to support actions that
	// require ApplicationsNamespace. If DSCI truly doesn't exist, the error will
	// propagate and the caller must handle appropriately.
	dsci, err := cluster.GetDSCI(ctx, rr.Client)
	if err != nil {
		return "", fmt.Errorf("ApplicationsNamespace not available, DSCI not found: %w", err)
	}

	return dsci.Spec.ApplicationsNamespace, nil
}

func OperatorNamespace(_ context.Context, _ *odhTypes.ReconciliationRequest) (string, error) {
	ns, err := cluster.GetOperatorNamespace()
	if err != nil {
		return "", err
	}

	return ns, nil
}
