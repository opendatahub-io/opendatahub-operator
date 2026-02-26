package helm

import (
	"context"
	"maps"

	engineTypes "github.com/k8s-manifest-kit/engine/pkg/types"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func (a *Action) annotationTransformer() engineTypes.Transformer {
	return func(ctx context.Context, object unstructured.Unstructured) (unstructured.Unstructured, error) {
		annotations := object.GetAnnotations()
		if annotations == nil {
			annotations = make(map[string]string)
		}
		maps.Copy(annotations, a.annotations)
		object.SetAnnotations(annotations)

		return object, nil
	}
}

func (a *Action) labelTransformer() engineTypes.Transformer {
	return func(ctx context.Context, object unstructured.Unstructured) (unstructured.Unstructured, error) {
		labels := object.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		maps.Copy(labels, a.labels)
		object.SetLabels(labels)

		return object, nil
	}
}
