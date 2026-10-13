package conditions

import (
	frameworkapi "github.com/opendatahub-io/odh-platform-utilities/framework/api"
	fwconditions "github.com/opendatahub-io/odh-platform-utilities/framework/controller/conditions"
	fwreconciler "github.com/opendatahub-io/odh-platform-utilities/framework/controller/reconciler"
)

// NewManager constructs a condition manager for operator tests using the
// framework's shared dependency conversion.
func NewManager(accessor frameworkapi.ConditionsAccessor, target string, dependencies ...string) *fwconditions.Manager {
	aggregator, err := fwconditions.NewAggregator(
		frameworkapi.ConditionType(target),
		fwreconciler.DependentConditions(dependencies...)...,
	)
	if err != nil {
		panic(err)
	}

	return fwconditions.NewManager(accessor, aggregator)
}
