package resources_test

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"

	. "github.com/onsi/gomega"
)

func TestHasAnnotationAndLabels(t *testing.T) {
	tests := []struct {
		name     string
		data     map[string]string
		key      string
		values   []string
		expected bool
	}{
		{"nil object", nil, "key1", []string{"val1"}, false},
		{"no metadata", map[string]string{}, "key1", []string{"val1"}, false},
		{"metadata exists and value matches", map[string]string{"key1": "val1"}, "key1", []string{"val1"}, true},
		{"metadata exists and value doesn't match", map[string]string{"key1": "val2"}, "key1", []string{"val1"}, false},
		{"metadata exists and value in list", map[string]string{"key1": "val2"}, "key1", []string{"val1", "val2"}, true},
		{"metadata exists and key doesn't match", map[string]string{"key2": "val1"}, "key1", []string{"val1"}, false},
		{"multiple values and no match", map[string]string{"key1": "val3"}, "key1", []string{"val1", "val2"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Run("annotations_"+tt.name, func(t *testing.T) {
				g := NewWithT(t)

				obj := unstructured.Unstructured{}
				if len(tt.data) != 0 {
					obj.SetAnnotations(tt.data)
				}

				result := resources.HasAnnotation(&obj, tt.key, tt.values...)

				g.Expect(result).To(Equal(tt.expected))
			})

			t.Run("labels_"+tt.name, func(t *testing.T) {
				g := NewWithT(t)

				obj := unstructured.Unstructured{}
				if len(tt.data) != 0 {
					obj.SetLabels(tt.data)
				}

				result := resources.HasLabel(&obj, tt.key, tt.values...)

				g.Expect(result).To(Equal(tt.expected))
			})
		})
	}
}
