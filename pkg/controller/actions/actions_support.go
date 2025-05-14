package actions

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"

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

func IfGVKInstalled(kvg schema.GroupVersionKind) func(context.Context, *odhTypes.ReconciliationRequest) bool {
	return func(ctx context.Context, rr *odhTypes.ReconciliationRequest) bool {
		hasCRD, err := cluster.HasCRD(ctx, rr.Client, kvg)
		if err != nil {
			ctrl.Log.Error(err, "error checking if CRD installed", "GVK", kvg)
			return false
		}
		return hasCRD
	}
}
