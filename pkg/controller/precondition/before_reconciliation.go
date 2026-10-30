package precondition

import (
	"context"
	"fmt"
	"slices"

	ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// BeforeReconciliation schedules a precondition after the primary resource is
// fetched, before finalizers, actions, or condition/status handling. Failure or
// error stops the entire attempt without condition reporting, independently of
// WithStopReconciliation and WithSeverity.
//
// Check and skip callbacks must be read-only: they must not mutate resources or
// the instance, emit Events, or access rr.Conditions, which is nil in this phase.
// Action-produced fields on the request are not yet populated.
func BeforeReconciliation() Option {
	return func(pc *PreCondition) {
		pc.beforeReconciliation = true
	}
}

// RunBeforeReconciliation evaluates only checks marked with BeforeReconciliation,
// in registration order, without accessing conditions. It returns the first
// failing result or error, or Pass true when all applicable checks pass or skip.
// The request contains the primary instance, client/controller, release, and
// paths; Conditions is nil and action-produced fields are unset.
func RunBeforeReconciliation(ctx context.Context, rr *types.ReconciliationRequest, preConditions []PreCondition) (CheckResult, error) {
	clusterType := cluster.GetClusterInfo().Type
	l := ctrlLog.FromContext(ctx)

	for i := range preConditions {
		pc := &preConditions[i]
		if !pc.beforeReconciliation {
			continue
		}
		if len(pc.clusterTypes) > 0 && !slices.Contains(pc.clusterTypes, clusterType) {
			continue
		}
		if pc.skipFunc != nil {
			skip, err := pc.skipFunc(ctx, rr)
			if err != nil {
				return CheckResult{}, fmt.Errorf("precondition %q before reconciliation: skip function failed: %w", pc.conditionType, err)
			}
			if skip {
				l.Info("Pre-condition skipped before reconciliation", "conditionType", pc.conditionType)
				continue
			}
		}
		if pc.check == nil {
			return CheckResult{}, fmt.Errorf("precondition %q before reconciliation: check function is nil", pc.conditionType)
		}

		result, err := pc.check(ctx, rr)
		if err != nil {
			return CheckResult{}, fmt.Errorf("precondition %q before reconciliation: check failed: %w", pc.conditionType, err)
		}
		if !result.Pass {
			if pc.message != "" {
				result.Message = pc.message
			}
			return result, nil
		}
	}

	return CheckResult{Pass: true}, nil
}
