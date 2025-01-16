package matchers

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

func ExtractStatusCondition(conditionType string) func(in types.ResourceObject) metav1.Condition {
	return func(in types.ResourceObject) metav1.Condition {
		c := meta.FindStatusCondition(in.GetStatus().Conditions, conditionType)
		if c == nil {
			return metav1.Condition{}
		}

		return *c
	}
}
