//nolint:testpackage
package jq

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"

	. "github.com/onsi/gomega"
)

func TestBytesToType(t *testing.T) {
	t.Parallel()

	g := NewGomegaWithT(t)

	tests := []struct {
		name     string
		input    []byte
		expected any
	}{
		{
			name:     "Empty Input",
			input:    []byte{},
			expected: nil,
		},
		{
			name:     "Valid JSON Object",
			input:    []byte(`{"key": "value"}`),
			expected: map[string]any{"key": "value"},
		},
		{
			name:     "Valid JSON Array",
			input:    []byte(`[1, "two", 3.0]`),
			expected: []any{float64(1), "two", 3.0},
		},
		{
			name:     "Invalid JSON",
			input:    []byte(`{invalid}`),
			expected: nil,
		},
		{
			name:     "Non-Object/Array JSON",
			input:    []byte(`"string"`),
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := byteToType(tt.input)

			if tt.expected == nil {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(result).To(Equal(tt.expected))
			}
		})
	}
}

func haveType(kind reflect.Kind) func(in any) bool {
	return func(in any) bool {
		return reflect.TypeOf(in).Kind() == kind
	}
}

func TestToType(t *testing.T) {
	t.Parallel()

	typeTestData := []byte(`{ "foo": "bar" }`)

	g := NewWithT(t)

	tests := []struct {
		name        string
		fn          func() any
		expectation types.GomegaMatcher
	}{
		{
			name: "gbytes",
			fn: func() any {
				b := gbytes.NewBuffer()

				_, err := b.Write(typeTestData)
				g.Expect(err).ShouldNot(HaveOccurred())

				return b
			},
			expectation: Satisfy(haveType(reflect.Map)),
		},
		{
			name: "bytes",
			fn: func() any {
				return typeTestData
			},
			expectation: Satisfy(haveType(reflect.Map)),
		},
		{
			name: "string_map",
			fn: func() any {
				return string(typeTestData)
			},
			expectation: Satisfy(haveType(reflect.Map)),
		},
		{
			name: "string_slice",
			fn: func() any {
				return `[ "foo", "bar" ]`
			},
			expectation: Satisfy(haveType(reflect.Slice)),
		},
		{
			name: "json.RawMessage",
			fn: func() any {
				return json.RawMessage(typeTestData)
			},
			expectation: Satisfy(haveType(reflect.Map)),
		},
		{
			name: "io.Reader",
			fn: func() any {
				return strings.NewReader(string(typeTestData))
			},
			expectation: Satisfy(haveType(reflect.Map)),
		},
		{
			name: "unstructured.Unstructured",
			fn: func() any {
				return *resources.GvkToUnstructured(gvk.ConfigMap)
			},
			expectation: Satisfy(haveType(reflect.Map)),
		},
		{
			name: "*unstructured.Unstructured",
			fn: func() any {
				return resources.GvkToUnstructured(gvk.ConfigMap)
			},
			expectation: Satisfy(haveType(reflect.Map)),
		},
		{
			name: "map",
			fn: func() any {
				return map[string]string{"foo": "bar"}
			},
			expectation: Satisfy(haveType(reflect.Map)),
		},
		{
			name: "slice",
			fn: func() any {
				return []string{"foo", "bar"}
			},
			expectation: Satisfy(haveType(reflect.Slice)),
		},
		{
			name: "*unstructured.Unstructured(nil)",
			fn: func() any {
				return (*unstructured.Unstructured)(nil)
			},
			expectation: BeNil(),
		},
		{
			name: "nil",
			fn: func() any {
				return nil
			},
			expectation: BeNil(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			convertedType, err := toType(tt.fn())

			g.Expect(err).ShouldNot(HaveOccurred())
			g.Expect(convertedType).Should(tt.expectation)
		})
	}
}
