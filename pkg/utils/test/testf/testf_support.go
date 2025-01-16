package testf

import (
	"fmt"

	"github.com/itchyny/gojq"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// StopErr stops the retry process with a specified message and wraps the provided error.
//
// This function leverages Gomega's StopTrying function to signal an end to retrying operations
// when a condition is not satisfied or an error occurs. It enhances the error output
// by wrapping the original error (if any) with the provided message.
//
// Parameters:
//   - err: An error to wrap.
//   - message: A string message that describes the reason for stopping retries.
//
// Returns:
//
//	An error that combines the stopping message and the wrapped error.
//
// Example usage:
//
//	err := someOperation()
//	if err != nil {
//	    return StopErr(err, "Operation failed")
//	}
func StopErr(err error, format string, args ...any) error {
	msg := format
	if len(args) != 0 {
		msg = fmt.Sprintf(format, args...)
	}

	return gomega.StopTrying(msg).Wrap(err)
}

// TransformFn defines a function type that takes an *unstructured.Unstructured object
// and applies a transformation to it. The function returns an error if the transformation fails.
type TransformFn func(obj *unstructured.Unstructured) error

// TransformPipeline constructs a composite TransformFn from a series of TransformFn steps.
// It returns a single TransformFn that applies each step sequentially to the given object.
//
// If any step returns an error, the pipeline terminates immediately and returns that error.
// If all steps succeed, the pipeline returns nil.
func TransformPipeline(steps ...TransformFn) TransformFn {
	return func(obj *unstructured.Unstructured) error {
		for _, step := range steps {
			err := step(obj)
			if err != nil {
				return err
			}
		}

		return nil
	}
}

// Transform creates a transformation function that applies a JQ-like query expression to an
// unstructured Kubernetes object (`unstructured.Unstructured`), allowing dynamic field extraction,
// modification, or replacement of the object's content.
//
// This function generates a transformation function by formatting a query string using
// the provided format and arguments. The returned function can be applied to an
// `*unstructured.Unstructured` object, which will be updated based on the result of the query.
//
// Parameters:
//   - format: A format string for building a JQ-like query expression.
//   - args: Variadic arguments to populate placeholders in the format string.
//
// Returns:
//   - func(*unstructured.Unstructured) error: A function that applies the formatted query to
//     the provided `*unstructured.Unstructured` object and updates its content.
func Transform(format string, args ...any) TransformFn {
	expression := fmt.Sprintf(format, args...)

	return func(in *unstructured.Unstructured) error {
		query, err := gojq.Parse(expression)
		if err != nil {
			return fmt.Errorf("unable to parse expression %q: %w", expression, err)
		}

		result, ok := query.Run(in.Object).Next()
		if !ok || result == nil {
			// No results or nil result, nothing to update
			return nil
		}

		if err, ok := result.(error); ok {
			return fmt.Errorf("query execution error: %w", err)
		}

		uc, ok := result.(map[string]interface{})
		if !ok {
			return fmt.Errorf("expected map[string]interface{}, got %T", result)
		}

		in.SetUnstructuredContent(uc)

		return nil
	}
}
