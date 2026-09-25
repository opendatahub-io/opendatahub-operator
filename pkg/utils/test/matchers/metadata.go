package matchers

import (
	"fmt"

	"github.com/onsi/gomega/types"
)

type metadataMatcher struct {
	kind   string
	key    string
	value  string
	values func(any) (map[string]string, bool)
}

// HaveAnnotation matches a Kubernetes object containing the expected annotation.
func HaveAnnotation(key, value string) types.GomegaMatcher {
	return metadataMatcher{
		kind:  "annotation",
		key:   key,
		value: value,
		values: func(actual any) (map[string]string, bool) {
			object, ok := actual.(interface{ GetAnnotations() map[string]string })
			if !ok {
				return nil, false
			}

			return object.GetAnnotations(), true
		},
	}
}

// HaveLabel matches a Kubernetes object containing the expected label.
func HaveLabel(key, value string) types.GomegaMatcher {
	return metadataMatcher{
		kind:  "label",
		key:   key,
		value: value,
		values: func(actual any) (map[string]string, bool) {
			object, ok := actual.(interface{ GetLabels() map[string]string })
			if !ok {
				return nil, false
			}

			return object.GetLabels(), true
		},
	}
}

func (matcher metadataMatcher) Match(actual any) (bool, error) {
	values, ok := matcher.values(actual)
	if !ok {
		return false, fmt.Errorf("have%s expects an object with metadata, got %T", matcher.kind, actual)
	}

	actualValue, found := values[matcher.key]
	return found && actualValue == matcher.value, nil
}

func (matcher metadataMatcher) FailureMessage(actual any) string {
	return fmt.Sprintf(
		"Expected %T to have %s %q with value %q, but got %#v",
		actual, matcher.kind, matcher.key, matcher.value, actual,
	)
}

func (matcher metadataMatcher) NegatedFailureMessage(actual any) string {
	return fmt.Sprintf(
		"Expected %T not to have %s %q with value %q",
		actual, matcher.kind, matcher.key, matcher.value,
	)
}
