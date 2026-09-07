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

// RequiredAPI declares an API resource that the operator will create or read.
// See [monitor.RequiredAPI].
type RequiredAPI = monitor.RequiredAPI

// MonitorCRD creates a PreCondition that checks a single CRD against the
// required-API contract. See [MonitorCRDs].
func MonitorCRD(crdName string, opts ...Option) PreCondition {
	return MonitorCRDs([]string{crdName}, opts...)
}

// MonitorCRDs creates a PreCondition that checks the given CRDs against the
// required-API contract: each CRD must exist, be Established, and not be
// Terminating. No API version is declared, so the served-version check is
// skipped; use [MonitorAPIs] to also assert that a specific version is served.
func MonitorCRDs(crdNames []string, opts ...Option) PreCondition {
	apis := make([]RequiredAPI, 0, len(crdNames))
	for _, name := range crdNames {
		apis = append(apis, RequiredAPI{CRDName: name})
	}

	return MonitorAPIs(apis, opts...)
}

// MonitorAPIs creates a PreCondition that checks that every declared API is
// usable, verifying CRD existence, the Established and Terminating status
// conditions, and — when the API declares a version — that the CRD serves it.
//
// A missing, terminating, or non-serving CRD reports the condition False. A CRD
// that is not yet Established reports Unknown, because that state may be either
// transient (still installing) or permanent (the CRD was rejected), and missing
// information must not be reported as healthy.
//
// See [monitor.CheckRequiredAPIs] for the full contract.
func MonitorAPIs(apis []RequiredAPI, opts ...Option) PreCondition {
	required := slices.Clone(apis)

	return newPreCondition(func(ctx context.Context, rr *types.ReconciliationRequest) (CheckResult, error) {
		return monitor.CheckRequiredAPIs(ctx, rr.Client, required)
	}, opts...)
}

// SkipIfCRDAbsent returns a [SkipFunc] that skips the precondition when the named
// CRD is not registered on the cluster.
//
// Use it for optional dependencies whose API only exists on some installations —
// for example an OpenShift-specific health CRD that community installations do
// not provide. When the CRD is present, the precondition runs normally.
//
// The CRD is read by name rather than through the RESTMapper, so a freshly
// installed CRD is not reported as absent while the discovery cache catches up.
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
