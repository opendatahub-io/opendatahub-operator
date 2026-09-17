package matchers

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

// SetAnnotation creates a function that sets an annotation on an unstructured object.
func SetAnnotation(key, value string) func(*unstructured.Unstructured) error {
	return func(in *unstructured.Unstructured) error {
		resources.SetAnnotation(in, key, value)
		return nil
	}
}

// SetLabel creates a function that sets a label on an unstructured object.
func SetLabel(key, value string) func(*unstructured.Unstructured) error {
	return func(in *unstructured.Unstructured) error {
		resources.SetLabel(in, key, value)
		return nil
	}
}

// RemoveAnnotation creates a function that removes an annotation from an unstructured object.
func RemoveAnnotation(key string) func(*unstructured.Unstructured) error {
	return func(in *unstructured.Unstructured) error {
		resources.RemoveAnnotation(in, key)
		return nil
	}
}

// RemoveLabel creates a function that removes a label from an unstructured object.
func RemoveLabel(key string) func(*unstructured.Unstructured) error {
	return func(in *unstructured.Unstructured) error {
		resources.RemoveLabel(in, key)
		return nil
	}
}

func ExtractStatusCondition(conditionType string) func(in types.ResourceObject) common.Condition {
	return func(in types.ResourceObject) common.Condition {
		c := conditions.FindStatusCondition(in.GetStatus(), conditionType)
		if c == nil {
			return common.Condition{}
		}

		return *c
	}
}
