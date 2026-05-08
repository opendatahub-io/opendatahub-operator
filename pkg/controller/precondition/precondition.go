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

// CheckFunc is the function signature for a pre-reconciliation check.
type CheckFunc func(ctx context.Context, rr *types.ReconciliationRequest) (CheckResult, error)

type Option func(*PreCondition)

// PreCondition composes a Check with framework configuration that controls
// how RunAll aggregates and writes Kubernetes status conditions.
type PreCondition struct {
	check              CheckFunc
	conditionType      string
	severity           common.ConditionSeverity
	stopReconciliation bool
	clusterTypes       []string
	message            string
}

// WithConditionType sets the condition type that will be written.
// Empty strings are ignored to preserve the constructor default.
func WithConditionType(t string) Option {
	return func(pc *PreCondition) {
		if t != "" {
			pc.conditionType = t
		}
	}
}

// WithSeverity sets the severity of the condition that will be written.
func WithSeverity(s common.ConditionSeverity) Option {
	return func(pc *PreCondition) {
		pc.severity = s
	}
}

// WithStopReconciliation flags the reconciliation to be stopped if the precondition is not met.
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

// WithMessage sets the message that will be written to the condition.
func WithMessage(msg string) Option {
	return func(pc *PreCondition) {
		pc.message = msg
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

// RunAll runs all the preconditions and returns true when the reconciliation should be stopped.
func RunAll(ctx context.Context, rr *types.ReconciliationRequest, preConditions []PreCondition) bool {
	if len(preConditions) == 0 {
		return false
	}

	l := ctrlLog.FromContext(ctx)
	clusterType := cluster.GetClusterInfo().Type
	results := make(map[string]*conditionAggregate)

	for i := range preConditions {
		pc := &preConditions[i]

		// Skip preconditions that don't apply to this cluster type.
		if len(pc.clusterTypes) > 0 && !slices.Contains(pc.clusterTypes, clusterType) {
			continue
		}

		// Initialize the condition aggregate for this condition type.
		if results[pc.conditionType] == nil {
			results[pc.conditionType] = &conditionAggregate{
				status:   metav1.ConditionTrue,
				severity: common.ConditionSeverityInfo,
			}
		}
		agg := results[pc.conditionType]

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
