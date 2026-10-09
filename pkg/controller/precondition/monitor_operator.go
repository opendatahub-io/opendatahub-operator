package precondition

import (
	"context"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/monitor"
	odhtypes "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

// ConditionFilterFunc is an alias for [monitor.ConditionFilterFunc].
type ConditionFilterFunc = monitor.ConditionFilterFunc

// OperatorConfig is an alias for [monitor.OperatorConfig].
type OperatorConfig = monitor.OperatorConfig

// RequiredCondition is an alias for [monitor.RequiredCondition].
type RequiredCondition = monitor.RequiredCondition

// MonitorOperator creates a PreCondition backed by [monitor.CheckOperatorHealth].
func MonitorOperator(config OperatorConfig, opts ...Option) PreCondition {
	return newPreCondition(func(ctx context.Context, rr *odhtypes.ReconciliationRequest) (CheckResult, error) {
		return monitor.CheckOperatorHealth(ctx, rr.Client, config)
	}, opts...)
}
