package jq

import (
	"fmt"
	"reflect"

	"github.com/itchyny/gojq"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Transform creates a function that applies a jq expression to an
// unstructured Kubernetes object and replaces the object's content with the
// transformed map.
func Transform(format string, args ...any) func(*unstructured.Unstructured) error {
	expression := fmt.Sprintf(format, args...)

	return func(in *unstructured.Unstructured) error {
		return transformObject(in, expression)
	}
}

func transformObject(in *unstructured.Unstructured, expression string) error {
	query, err := gojq.Parse(expression)
	if err != nil {
		return fmt.Errorf("unable to parse expression %q: %w", expression, err)
	}

	result, ok := query.Run(in.Object).Next()
	if !ok || result == nil {
		return nil
	}

	if err, ok := result.(error); ok {
		return fmt.Errorf("query execution error: %w", err)
	}

	content, ok := result.(map[string]any)
	if !ok {
		return fmt.Errorf("expected map[string]interface{}, got %T", result)
	}

	in.SetUnstructuredContent(content)

	return nil
}

func Extract(expression string) func(in any) (any, error) {
	return func(in any) (any, error) {
		return ExtractValue[any](in, expression)
	}
}

func ExtractValue[T any](in any, expression string) (T, error) {
	var result T
	var ok bool

	query, err := gojq.Parse(expression)
	if err != nil {
		return result, fmt.Errorf("unable to parse expression %s, %w", expression, err)
	}

	data, err := toType(in)
	if err != nil {
		return result, err
	}

	it := query.Run(data)

	v, ok := it.Next()
	if !ok {
		return result, nil
	}

	if err, ok := v.(error); ok {
		return result, err
	}

	result, ok = v.(T)
	if !ok {
		// JSON unmarshaling represents all numbers as float64. Attempt
		// numeric conversion so callers can use ExtractValue[int] and
		// similar integer types without knowing about this detail.
		rv := reflect.ValueOf(v)
		rt := reflect.TypeFor[T]()

		if rv.CanConvert(rt) {
			result, _ = rv.Convert(rt).Interface().(T)
		} else {
			return result, fmt.Errorf("result value is not of the expected type (expected:%T, got:%T", result, v)
		}
	}

	return result, nil
}
