package jq_test

import (
	"encoding/json"
	"testing"

	"github.com/onsi/gomega/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

// TestMatcher verifies the jq.Match function against various JSON strings.
// It ensures correct behavior for simple key-value matches, array value checks,
// handling of null values, and nested object conditions.
func TestMatcher(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	// Define multiple test cases for jq matching
	testCases := []struct {
		input   string
		matcher types.GomegaMatcher
	}{
		{
			// Test matching a simple value
			input:   `{"a":1}`,
			matcher: jq.Match(`.a == 1`),
		},
		{
			// Test when the value doesn't match
			input:   `{"a":1}`,
			matcher: Not(jq.Match(`.a == 2`)),
		},
		{
			// Test array with values matching
			input:   `{"Values":[ "foo" ]}`,
			matcher: jq.Match(`.Values | if . then any(. == "foo") else false end`),
		},
		{
			// Test array with non-matching value
			input:   `{"Values":[ "foo" ]}`,
			matcher: Not(jq.Match(`.Values | if . then any(. == "bar") else false end`)),
		},
		{
			// Test when the value is null
			input:   `{"Values": null}`,
			matcher: Not(jq.Match(`.Values | if . then any(. == "foo") else false end`)),
		},
		{
			// Test multiple matching conditions
			input:   `{ "status": { "foo": { "bar": "fr", "baz": "fb" } } }`,
			matcher: jq.Match(`(.status.foo.bar == "fr") and (.status.foo.baz == "fb")`),
		},
	}

	// Run the test cases
	for _, tc := range testCases {
		g.Expect(tc.input).Should(tc.matcher)
	}
}

// TestMatcherWithType validates jq.Match against different Go data types such as maps and structs.
// It ensures that jq.Match works correctly after serializing Go types to JSON.
func TestMatcherWithType(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	// Define multiple test cases for jq matching
	testCases := []struct {
		input   interface{}
		matcher types.GomegaMatcher
	}{
		{
			input:   map[string]any{"a": 1},
			matcher: WithTransform(json.Marshal, jq.Match(`.a == 1`)),
		},
		{
			input: map[string]any{
				"status": map[string]any{
					"foo": map[string]any{
						"bar": "fr",
						"baz": "fb",
					},
				},
			},
			matcher: WithTransform(json.Marshal, And(
				jq.Match(`.status.foo.bar == "fr"`),
				jq.Match(`.status.foo.baz == "fb"`),
			)),
		},
		{
			input:   map[string]any{"a": 1},
			matcher: jq.Match(`.a == 1`),
		},
		{
			input: struct {
				A int `json:"a"`
			}{A: 1},
			matcher: WithTransform(json.Marshal, jq.Match(`.a == 1`)),
		},
	}

	// Run the test cases
	for _, tc := range testCases {
		g.Expect(tc.input).Should(tc.matcher)
	}
}

func TestUnstructuredSliceMatcher(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)

	u := []unstructured.Unstructured{{
		Object: map[string]interface{}{
			"a": 1,
		}},
	}

	// Define multiple test cases for jq matching
	testCases := []struct {
		input   interface{}
		matcher types.GomegaMatcher
	}{
		{
			input:   u,
			matcher: jq.Match(`.[0] | .a == 1`),
		},
		{
			input:   unstructured.UnstructuredList{Items: u},
			matcher: jq.Match(`.[0] | .a == 1`),
		},
	}

	// Run the test cases
	for _, tc := range testCases {
		g.Expect(tc.input).Should(tc.matcher)
	}
}
