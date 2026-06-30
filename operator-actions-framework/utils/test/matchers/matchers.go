package matchers

import (
	"github.com/opendatahub-io/operator-actions-framework/api"
	"github.com/opendatahub-io/operator-actions-framework/controller/conditions"
	"github.com/opendatahub-io/operator-actions-framework/controller/types"
)

func ExtractStatusCondition(conditionType string) func(in types.ResourceObject) api.Condition {
	return func(in types.ResourceObject) api.Condition {
		c := conditions.FindStatusCondition(in.GetStatus(), conditionType)
		if c == nil {
			return api.Condition{}
		}

		return *c
	}
}
