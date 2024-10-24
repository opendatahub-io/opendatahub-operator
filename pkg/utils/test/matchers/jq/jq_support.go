package jq

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/gbytes"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func formattedMessage(comparisonMessage string, failurePath []interface{}) string {
	diffMessage := ""

	if len(failurePath) != 0 {
		diffMessage = "\n\nfirst mismatched key: " + formattedFailurePath(failurePath)
	}

	return comparisonMessage + diffMessage
}

func formattedFailurePath(failurePath []interface{}) string {
	formattedPaths := make([]string, 0)

	for i := len(failurePath) - 1; i >= 0; i-- {
		switch p := failurePath[i].(type) {
		case int:
			val := fmt.Sprintf(`[%d]`, p)
			formattedPaths = append(formattedPaths, val)
		default:
			if i != len(failurePath)-1 {
				formattedPaths = append(formattedPaths, ".")
			}

			val := fmt.Sprintf(`"%s"`, p)
			formattedPaths = append(formattedPaths, val)
		}
	}

	return strings.Join(formattedPaths, "")
}

//nolint:cyclop
func toType(in any) (any, error) {
	switch v := in.(type) {
	case string:
		d, err := byteToType([]byte(v))
		if err != nil {
			return nil, err
		}

		return d, nil
	case []byte:
		d, err := byteToType(v)
		if err != nil {
			return nil, err
		}

		return d, nil
	case json.RawMessage:
		d, err := byteToType(v)
		if err != nil {
			return nil, err
		}

		return d, nil
	case *gbytes.Buffer:
		d, err := byteToType(v.Contents())
		if err != nil {
			return nil, err
		}

		return d, nil
	case io.Reader:
		data, err := io.ReadAll(v)
		if err != nil {
			return nil, fmt.Errorf("failed to read from reader: %w", err)
		}

		d, err := byteToType(data)
		if err != nil {
			return nil, err
		}

		return d, nil
	case unstructured.Unstructured:
		return v.Object, nil
	case *unstructured.Unstructured:
		return v.Object, nil
	}

	switch reflect.TypeOf(in).Kind() {
	case reflect.Map:
		return in, nil
	case reflect.Slice:
		return in, nil
	default:
		return nil, fmt.Errorf("unsuported type:\n%s", format.Object(in, 1))
	}
}

func byteToType(in []byte) (any, error) {
	if len(in) == 0 {
		return nil, errors.New("a valid Json document is expected")
	}

	switch in[0] {
	case '{':
		data := make(map[string]any)
		if err := json.Unmarshal(in, &data); err != nil {
			return nil, fmt.Errorf("unable to unmarshal result, %w", err)
		}

		return data, nil
	case '[':
		var data []any
		if err := json.Unmarshal(in, &data); err != nil {
			return nil, fmt.Errorf("unable to unmarshal result, %w", err)
		}

		return data, nil
	default:
		return nil, errors.New("a Json Array or Object is required")
	}
}
