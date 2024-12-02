package jq

import (
	"fmt"

	"github.com/itchyny/gojq"
)

func Extract(expression string) func(in any) (any, error) {
	return func(in any) (any, error) {
		query, err := gojq.Parse(expression)
		if err != nil {
			return nil, fmt.Errorf("unable to parse expression %s, %w", expression, err)
		}

		data, err := toType(in)
		if err != nil {
			return false, err
		}

		it := query.Run(data)

		v, ok := it.Next()
		if !ok {
			return false, nil
		}

		if err, ok := v.(error); ok {
			return false, err
		}

		return v, nil
	}
}
