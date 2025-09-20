package actions

import (
	"context"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	odhTypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

func ApplicationNamespace(_ context.Context, rr *odhTypes.ReconciliationRequest) (string, error) {
	return rr.DSCI.Spec.ApplicationsNamespace, nil
}

func OperatorNamespace(_ context.Context, _ *odhTypes.ReconciliationRequest) (string, error) {
	ns, err := cluster.GetOperatorNamespace()
	if err != nil {
		return "", err
	}

	return ns, nil
}
