package precondition

import (
	"context"
	"slices"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	cond "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

const PreConditionFailedReason = "PreConditionFailed"

// CheckResult holds the outcome of a check execution.
type CheckResult struct {
	Pass    bool
	Message string
}

// CheckFunc is the function signature for a precondition check.
// Checks marked with BeforeReconciliation must follow its read-only contract.
type CheckFunc func(ctx context.Context, rr *types.ReconciliationRequest) (CheckResult, error)

type Option func(*PreCondition)

// SkipFunc is a runtime predicate evaluated in the precondition's execution
// phase after WithClusterTypes filtering. Return true to skip the check.
// In ordinary execution, errors become Unknown conditions and evaluation
// continues with the remaining preconditions (as with CheckFunc errors).
// RunBeforeReconciliation instead returns errors immediately without reporting
// conditions. Its skip callbacks must follow BeforeReconciliation's read-only contract.
type SkipFunc func(ctx context.Context, rr *types.ReconciliationRequest) (bool, error)

// PreCondition composes a check with execution and condition-reporting options.
// RunAll reports conditions for ordinary checks; RunBeforeReconciliation runs
// checks marked with BeforeReconciliation without condition reporting.
type PreCondition struct {
	check                CheckFunc
	conditionType        string
	severity             common.ConditionSeverity
	stopReconciliation   bool
	beforeReconciliation bool
	clusterTypes         []string
	message              string
	skipFunc             SkipFunc // runtime predicate to conditionally skip this precondition
}

// WithConditionType sets the condition type reported during ordinary execution.
// Empty strings are ignored to preserve the constructor default.
func WithConditionType(t string) Option {
	return func(pc *PreCondition) {
		if t != "" {
			pc.conditionType = t
		}
	}
}

// WithSeverity sets the condition severity reported during ordinary execution.
func WithSeverity(s common.ConditionSeverity) Option {
	return func(pc *PreCondition) {
		pc.severity = s
	}
}

// WithStopReconciliation stops actions when an ordinary precondition fails,
// while still allowing status reporting. BeforeReconciliation checks always
// stop the entire attempt on failure or error, independently of this option.
func WithStopReconciliation() Option {
	return func(pc *PreCondition) {
		pc.stopReconciliation = true
	}
}

// WithClusterTypes sets the cluster types on which the precondition will be checked.
func WithClusterTypes(types ...string) Option {
	return func(pc *PreCondition) {
		pc.clusterTypes = slices.Clone(types)
	}
}

// WithMessage overrides the failure message reported in a condition during
// ordinary execution or returned by RunBeforeReconciliation.
func WithMessage(msg string) Option {
	return func(pc *PreCondition) {
		pc.message = msg
	}
}

// WithSkipFunc sets a predicate evaluated in the precondition's execution phase
// after WithClusterTypes filtering. See [SkipFunc] for phase-specific error handling.
func WithSkipFunc(fn SkipFunc) Option {
	return func(pc *PreCondition) {
		pc.skipFunc = fn
	}
}

func newPreCondition(check CheckFunc, opts ...Option) PreCondition {
	pc := PreCondition{
		check:         check,
		conditionType: status.ConditionDependenciesAvailable,
		severity:      common.ConditionSeverityError,
	}

	for _, opt := range opts {
		opt(&pc)
	}

	return pc
}

func initAggregate(results map[string]*conditionAggregate, conditionType string) *conditionAggregate {
	if results[conditionType] == nil {
		results[conditionType] = &conditionAggregate{
			status:   metav1.ConditionTrue,
			severity: common.ConditionSeverityInfo,
		}
	}
	return results[conditionType]
}

// conditionAggregate aggregates the check results for a given condition type.
type conditionAggregate struct {
	status     metav1.ConditionStatus
	severity   common.ConditionSeverity
	messages   []string
	shouldStop bool
}

// Priority: False > Unknown > True.
func (agg *conditionAggregate) record(s metav1.ConditionStatus, message string, pc *PreCondition) {
	switch {
	case s == metav1.ConditionFalse:
		agg.status = metav1.ConditionFalse
	case s == metav1.ConditionUnknown && agg.status != metav1.ConditionFalse:
		agg.status = metav1.ConditionUnknown
	}

	agg.messages = append(agg.messages, message)

	if pc.severity == common.ConditionSeverityError {
		agg.severity = common.ConditionSeverityError
	}

	if pc.stopReconciliation {
		agg.shouldStop = true
	}
}

// RunAll runs ordinary preconditions and reports their aggregated conditions.
// It skips checks marked with BeforeReconciliation. It returns true when
// reconciliation actions should stop; status reporting can still proceed.
func RunAll(ctx context.Context, rr *types.ReconciliationRequest, preConditions []PreCondition) bool {
	if len(preConditions) == 0 {
		return false
	}

	l := ctrlLog.FromContext(ctx)
	clusterType := cluster.GetClusterInfo().Type
	results := make(map[string]*conditionAggregate)

	for i := range preConditions {
		pc := &preConditions[i]
		if pc.beforeReconciliation {
			continue
		}

		// Skip preconditions that don't apply to this cluster type.
		// Initialize the aggregate so the condition is written as True
		// (not-applicable = satisfied) rather than left unset.
		if len(pc.clusterTypes) > 0 && !slices.Contains(pc.clusterTypes, clusterType) {
			initAggregate(results, pc.conditionType)
			continue
		}

		if pc.skipFunc != nil {
			skip, skipErr := pc.skipFunc(ctx, rr)
			if skipErr != nil {
				l.Info("Pre-condition skip function error", "conditionType", pc.conditionType, "error", skipErr.Error())

				initAggregate(results, pc.conditionType).record(metav1.ConditionUnknown, skipErr.Error(), pc)

				continue
			}

			if skip {
				l.Info("Pre-condition skipped by runtime predicate", "conditionType", pc.conditionType)
				initAggregate(results, pc.conditionType)

				continue
			}
		}

		agg := initAggregate(results, pc.conditionType)

		if pc.check == nil {
			l.Info("Pre-condition check function is nil", "conditionType", pc.conditionType)
			agg.record(metav1.ConditionUnknown, "precondition check function is nil", pc)

			continue
		}

		// Run the precondition check.
		result, err := pc.check(ctx, rr)
		if err != nil {
			l.Info("Pre-condition check error", "conditionType", pc.conditionType, "error", err.Error())
			agg.record(metav1.ConditionUnknown, err.Error(), pc)

			continue
		}

		if !result.Pass {
			l.Info("Pre-condition not met", "conditionType", pc.conditionType, "message", result.Message)

			msg := result.Message
			if pc.message != "" {
				msg = pc.message
			}

			agg.record(metav1.ConditionFalse, msg, pc)
		}
	}

	// Write aggregated results to conditions.
	shouldStop := false

	for ct, agg := range results {
		opts := []cond.Option{
			cond.WithObservedGeneration(rr.Instance.GetGeneration()),
		}

		if agg.status != metav1.ConditionTrue {
			opts = append(opts,
				cond.WithReason(PreConditionFailedReason),
				cond.WithSeverity(agg.severity),
				cond.WithMessage("%s", strings.Join(agg.messages, "; ")),
			)
		}

		rr.Conditions.Mark(ct, agg.status, opts...)

		if agg.shouldStop {
			shouldStop = true
		}
	}

	return shouldStop
}
