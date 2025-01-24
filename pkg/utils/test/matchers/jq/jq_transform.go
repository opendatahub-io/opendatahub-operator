package jq

import (
	"fmt"

	"github.com/itchyny/gojq"
)

func Extract(format string, args ...any) func(in any) (any, error) {
	return func(in any) (any, error) {
		return ExtractValue[any](in, format, args...)
	}
}

func ExtractValue[T any](in any, format string, args ...any) (T, error) {
	expression := format
	if len(args) > 0 {
		expression = fmt.Sprintf(format, args...)
	}

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
		return result, fmt.Errorf("result value is not of the expected type (expected:%T, got:%T", result, v)
	}

	return result, nil
}
