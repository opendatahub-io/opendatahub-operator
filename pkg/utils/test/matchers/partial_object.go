package matchers

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/onsi/gomega/types"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
)

type partialObjectMatcher struct {
	expected client.Object
}

// MatchPartialObject matches a Kubernetes object using Kubernetes deep-derivative semantics.
func MatchPartialObject(expected client.Object) types.GomegaMatcher {
	return &partialObjectMatcher{expected: expected}
}

func (matcher *partialObjectMatcher) Match(actual any) (bool, error) {
	if matcher.expected == nil {
		return false, errors.New("MatchPartialObject expected object must not be nil")
	}

	expected, err := expectedObjectMap(matcher.expected)
	if err != nil {
		return false, fmt.Errorf("failed to convert expected object: %w", err)
	}

	actualObject, ok := actual.(client.Object)
	if !ok {
		return false, fmt.Errorf("MatchPartialObject expects client.Object, got %T", actual)
	}
	if actualObject == nil {
		return false, nil
	}

	actualMap, err := objectMap(actualObject)
	if err != nil {
		return false, fmt.Errorf("failed to convert actual object: %w", err)
	}

	return apiequality.Semantic.DeepDerivative(expected, actualMap), nil
}

func (matcher *partialObjectMatcher) FailureMessage(actual any) string {
	return fmt.Sprintf(
		"Expected:\n\n%s\n\nto contain:\n\n%s\n\nactual object contained:\n\n%s",
		matcher.formatReference(),
		matcher.formatObject(matcher.expected),
		matcher.formatActual(actual),
	)
}

func (matcher *partialObjectMatcher) NegatedFailureMessage(actual any) string {
	return fmt.Sprintf(
		"Expected:\n\n%s\n\nnot to contain:\n\n%s\n\nactual object contained:\n\n%s",
		matcher.formatReference(),
		matcher.formatObject(matcher.expected),
		matcher.formatActual(actual),
	)
}

func (matcher *partialObjectMatcher) formatActual(actual any) string {
	object, ok := actual.(client.Object)
	if !ok || object == nil {
		return fmt.Sprintf("%#v", actual)
	}

	return matcher.formatObject(object)
}

func (*partialObjectMatcher) formatObject(object client.Object) string {
	if object == nil {
		return "<nil>"
	}

	data, err := yaml.Marshal(object)
	if err != nil {
		return fmt.Sprintf("%#v", object)
	}

	return string(data)
}

func (matcher *partialObjectMatcher) formatReference() string {
	object, err := resources.ToUnstructured(matcher.expected)
	if err != nil {
		return fmt.Sprintf("%T", matcher.expected)
	}

	return resources.FormatObjectReference(object)
}

func objectMap(object client.Object) (map[string]any, error) {
	unstructuredObject, err := resources.ToUnstructured(object)
	if err != nil {
		return nil, err
	}

	return unstructuredObject.Object, nil
}

func expectedObjectMap(object client.Object) (map[string]any, error) {
	objectMap, err := objectMap(object)
	if err != nil {
		return nil, err
	}

	if _, isUnstructured := object.(*unstructured.Unstructured); !isUnstructured {
		pruneZeroValues(objectMap)
	}

	return objectMap, nil
}

func pruneZeroValues(value any) bool {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			if pruneZeroValues(child) {
				delete(value, key)
			}
		}
		return len(value) == 0
	case []any:
		return len(value) == 0
	default:
		if value == nil {
			return true
		}

		return reflect.ValueOf(value).IsZero()
	}
}
