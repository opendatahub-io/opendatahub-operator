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

const (
	preConditionFailedReason = "PreConditionFailed"
)

type CheckResult struct {
	Pass    bool
	Message string
}

// Returning an error signals that the check itself could not complete
// (e.g. API unreachable), which maps to an `Unknown` condition.
type CheckFn func(ctx context.Context, rr *types.ReconciliationRequest) (CheckResult, error)

type Option func(*PreCondition)

type PreCondition struct {
	Check              CheckFn
	ConditionType      string
	Severity           common.ConditionSeverity
	StopReconciliation bool
	ClusterTypes       []string
	Message            string
}

// WithConditionType sets the condition type that will be written.
func WithConditionType(t string) Option {
	return func(pc *PreCondition) {
		pc.ConditionType = t
	}
}

// WithSeverity sets the severity of the condition that will be written.
func WithSeverity(s common.ConditionSeverity) Option {
	return func(pc *PreCondition) {
		pc.Severity = s
	}
}

// WithStopReconciliation flags the reconciliation to be stopped if the precondition is not met.
func WithStopReconciliation() Option {
	return func(pc *PreCondition) {
		pc.StopReconciliation = true
	}
}

// WithClusterTypes sets the cluster types on which the precondition will be checked.
func WithClusterTypes(types ...string) Option {
	return func(pc *PreCondition) {
		pc.ClusterTypes = types
	}
}

// WithMessage sets the message that will be written to the condition.
func WithMessage(msg string) Option {
	return func(pc *PreCondition) {
		pc.Message = msg
	}
}

func New(check CheckFn, opts ...Option) PreCondition {
	pc := PreCondition{
		Check:         check,
		ConditionType: status.ConditionDependenciesAvailable,
		Severity:      common.ConditionSeverityError,
	}

	for _, opt := range opts {
		opt(&pc)
	}

	return pc
}

// conditionAgg aggregates the check results for a given condition type.
type conditionAgg struct {
	status     metav1.ConditionStatus
	severity   common.ConditionSeverity
	messages   []string
	shouldStop bool
}

// RunAll runs all the preconditions and returns true when the reconciliation should be stopped.
func RunAll(ctx context.Context, rr *types.ReconciliationRequest, preConditions []PreCondition) bool {
	if len(preConditions) == 0 {
		return false
	}

	l := ctrlLog.FromContext(ctx)
	clusterType := cluster.GetClusterInfo().Type
	results := make(map[string]*conditionAgg)

	for i := range preConditions {
		pc := &preConditions[i]

		if len(pc.ClusterTypes) > 0 && !slices.Contains(pc.ClusterTypes, clusterType) {
			// Skip the precondition if the cluster type does not match.
			continue
		}

		if results[pc.ConditionType] == nil {
			// Initialize the condition aggregation for the condition type if it doesn't exist.
			results[pc.ConditionType] = &conditionAgg{
				status:   metav1.ConditionTrue,
				severity: common.ConditionSeverityInfo,
			}
		}
		agg := results[pc.ConditionType]

		if pc.Check == nil {
			l.Info("Pre-condition check function is nil", "conditionType", pc.ConditionType)

			if agg.status != metav1.ConditionFalse {
				agg.status = metav1.ConditionUnknown
			}
			agg.messages = append(agg.messages, "precondition check function is nil")

			if pc.Severity == common.ConditionSeverityError {
				agg.severity = common.ConditionSeverityError
			}

			if pc.StopReconciliation {
				agg.shouldStop = true
			}

			continue
		}

		// Run the precondition check.
		result, err := pc.Check(ctx, rr)
		if err != nil {
			l.Info("Pre-condition check error", "conditionType", pc.ConditionType, "error", err.Error())

			if agg.status != metav1.ConditionFalse {
				agg.status = metav1.ConditionUnknown
			}
			agg.messages = append(agg.messages, err.Error())

			if pc.Severity == common.ConditionSeverityError {
				agg.severity = common.ConditionSeverityError
			}

			if pc.StopReconciliation {
				agg.shouldStop = true
			}

			continue
		}

		if !result.Pass {
			l.Info("Pre-condition not met", "conditionType", pc.ConditionType, "message", result.Message)

			agg.status = metav1.ConditionFalse

			msg := result.Message
			if pc.Message != "" {
				msg = pc.Message
			}
			agg.messages = append(agg.messages, msg)

			if pc.Severity == common.ConditionSeverityError {
				agg.severity = common.ConditionSeverityError
			}

			if pc.StopReconciliation {
				agg.shouldStop = true
			}
		}
	}

	shouldStop := false

	// Set all conditions based on the aggregation results,
	// and check if the reconciliation should be stopped.
	for ct, agg := range results {
		var opts []cond.Option
		if agg.status != metav1.ConditionTrue {
			opts = append(opts,
				cond.WithReason(preConditionFailedReason),
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
