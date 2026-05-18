package jq

import (
	"fmt"
	"reflect"

	"github.com/itchyny/gojq"
)

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
