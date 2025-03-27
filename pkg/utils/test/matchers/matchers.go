package matchers

import (
	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

func ExtractStatusCondition(conditionType string) func(in types.ResourceObject) common.Condition {
	return func(in types.ResourceObject) common.Condition {
		c := conditions.FindStatusCondition(in.GetStatus(), conditionType)
		if c == nil {
			return common.Condition{}
		}

		return *c
	}
}
