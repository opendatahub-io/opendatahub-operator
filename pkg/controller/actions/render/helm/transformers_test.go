//nolint:testpackage // testing unexported methods
package helm

import (
	"context"
	"testing"

	engineTypes "github.com/k8s-manifest-kit/engine/pkg/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	. "github.com/onsi/gomega"
)

func TestMetadataTransformers(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		newAction   func(values map[string]string) *Action
		transformer func(a *Action) engineTypes.Transformer
		getter      func(u unstructured.Unstructured) map[string]string
		metadataKey string
	}{
		{
			name:        "annotation",
			newAction:   func(values map[string]string) *Action { return &Action{annotations: values} },
			transformer: func(a *Action) engineTypes.Transformer { return a.annotationTransformer() },
			getter:      func(u unstructured.Unstructured) map[string]string { return u.GetAnnotations() },
			metadataKey: "annotations",
		},
		{
			name:        "label",
			newAction:   func(values map[string]string) *Action { return &Action{labels: values} },
			transformer: func(a *Action) engineTypes.Transformer { return a.labelTransformer() },
			getter:      func(u unstructured.Unstructured) map[string]string { return u.GetLabels() },
			metadataKey: "labels",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Run("adds to object without existing metadata", func(t *testing.T) {
				g := NewWithT(t)

				action := tt.newAction(map[string]string{"key1": "value1", "key2": "value2"})

				obj := unstructured.Unstructured{
					Object: map[string]any{
						"apiVersion": "v1",
						"kind":       "ConfigMap",
						"metadata": map[string]any{
							"name": "test",
						},
					},
				}

				transformer := tt.transformer(action)
				result, err := transformer(ctx, obj)

				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(tt.getter(result)).Should(Equal(map[string]string{"key1": "value1", "key2": "value2"}))
			})

			t.Run("merges with existing metadata", func(t *testing.T) {
				g := NewWithT(t)

				action := tt.newAction(map[string]string{"new-key": "new-value"})

				obj := unstructured.Unstructured{
					Object: map[string]any{
						"apiVersion": "v1",
						"kind":       "ConfigMap",
						"metadata": map[string]any{
							"name": "test",
							tt.metadataKey: map[string]any{
								"existing-key": "existing-value",
							},
						},
					},
				}

				transformer := tt.transformer(action)
				result, err := transformer(ctx, obj)

				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(tt.getter(result)).Should(Equal(map[string]string{"existing-key": "existing-value", "new-key": "new-value"}))
			})

			t.Run("overwrites existing metadata with same key", func(t *testing.T) {
				g := NewWithT(t)

				action := tt.newAction(map[string]string{"key": "new-value"})

				obj := unstructured.Unstructured{
					Object: map[string]any{
						"apiVersion": "v1",
						"kind":       "ConfigMap",
						"metadata": map[string]any{
							"name": "test",
							tt.metadataKey: map[string]any{
								"key": "old-value",
							},
						},
					},
				}

				transformer := tt.transformer(action)
				result, err := transformer(ctx, obj)

				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(tt.getter(result)).Should(Equal(map[string]string{"key": "new-value"}))
			})
		})
	}
}
