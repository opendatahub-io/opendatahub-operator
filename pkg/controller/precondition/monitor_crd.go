package precondition

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	apihelpers "k8s.io/apiextensions-apiserver/pkg/apihelpers"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

func MonitorCRD(crdName string, opts ...Option) PreCondition {
	return MonitorCRDs([]string{crdName}, opts...)
}

// MonitorCRDs creates a PreCondition that checks for the presence of multiple CRDs
// by fetching the CRD objects directly, bypassing the RESTMapper discovery endpoint.
// This avoids a race where the K8s DiscoveryController lags behind the
// EstablishingController, causing the RESTMapper to report a CRD as absent
// even though it is Established.
func MonitorCRDs(crdNames []string, opts ...Option) PreCondition {
	names := slices.Clone(crdNames)

	return newPreCondition(func(ctx context.Context, rr *types.ReconciliationRequest) (CheckResult, error) {
		if len(names) == 0 {
			return CheckResult{}, errors.New("MonitorCRDs called with empty CRD name list")
		}

		var missing []string

		for _, name := range names {
			has, err := hasCRDByName(ctx, rr.Client, name)
			if err != nil {
				return CheckResult{}, fmt.Errorf("%s: failed to check CRD presence: %w", name, err)
			}

			if !has {
				missing = append(missing, name+": CRD not found")
			}
		}

		if len(missing) > 0 {
			return CheckResult{Pass: false, Message: strings.Join(missing, "; ")}, nil
		}

		return CheckResult{Pass: true}, nil
	}, opts...)
}

func hasCRDByName(ctx context.Context, cli client.Client, crdName string) (bool, error) {
	crd, err := cluster.GetCRD(ctx, cli, crdName)
	if err != nil {
		return false, client.IgnoreNotFound(err)
	}

	if apihelpers.IsCRDConditionTrue(&crd, apiextensionsv1.Terminating) {
		return false, nil
	}

	return true, nil
}
