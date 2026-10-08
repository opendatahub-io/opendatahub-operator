package precondition

import (
	"context"
	"fmt"
	"slices"

	k8serr "k8s.io/apimachinery/pkg/api/errors"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/monitor"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// RequiredAPI is an alias for [monitor.RequiredAPI].
type RequiredAPI = monitor.RequiredAPI

// MonitorCRD creates a PreCondition for one CRD.
func MonitorCRD(crdName string, opts ...Option) PreCondition {
	return MonitorCRDs([]string{crdName}, opts...)
}

// MonitorCRDs checks CRD lifecycle only; use [MonitorAPIs] to require a served version.
func MonitorCRDs(crdNames []string, opts ...Option) PreCondition {
	apis := make([]RequiredAPI, 0, len(crdNames))
	for _, name := range crdNames {
		apis = append(apis, RequiredAPI{CRDName: name})
	}

	return MonitorAPIs(apis, opts...)
}

// MonitorAPIs creates a PreCondition backed by [monitor.CheckRequiredAPIs].
func MonitorAPIs(apis []RequiredAPI, opts ...Option) PreCondition {
	required := slices.Clone(apis)

	return newPreCondition(func(ctx context.Context, rr *types.ReconciliationRequest) (CheckResult, error) {
		return monitor.CheckRequiredAPIs(ctx, rr.Client, required)
	}, opts...)
}

// SkipIfCRDAbsent skips optional APIs. It reads the CRD by name rather than through
// the RESTMapper, avoiding discovery-cache lag immediately after installation.
func SkipIfCRDAbsent(crdName string) SkipFunc {
	return func(ctx context.Context, rr *types.ReconciliationRequest) (bool, error) {
		_, err := cluster.GetCRD(ctx, rr.Client, crdName)
		switch {
		case k8serr.IsNotFound(err):
			return true, nil
		case err != nil:
			return false, fmt.Errorf("%s: failed to check CRD presence: %w", crdName, err)
		}

		return false, nil
	}
}
