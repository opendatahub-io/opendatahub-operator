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

// MonitorOperator creates a PreCondition that checks an external operator's health
// by reading its CR's status conditions and applying the configured Filter.
// See [monitor.OperatorConfig] for configuration details including missing CRD/CR behavior.
func MonitorOperator(config OperatorConfig, opts ...Option) PreCondition {
	return newPreCondition(func(ctx context.Context, rr *odhtypes.ReconciliationRequest) (CheckResult, error) {
		result, err := monitor.CheckOperatorHealth(ctx, rr.Client, config)

		return CheckResult(result), err
	}, opts...)
}
