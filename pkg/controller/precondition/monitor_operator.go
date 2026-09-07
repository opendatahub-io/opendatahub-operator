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

// MonitorOperator creates a PreCondition that checks an external operator's health
// by reading its CR's status conditions, applying the configured Filter and
// asserting the configured RequiredConditions.
// See [monitor.OperatorConfig] for configuration details including missing CRD/CR behavior.
func MonitorOperator(config OperatorConfig, opts ...Option) PreCondition {
	return newPreCondition(func(ctx context.Context, rr *odhtypes.ReconciliationRequest) (CheckResult, error) {
		return monitor.CheckOperatorHealth(ctx, rr.Client, config)
	}, opts...)
}
