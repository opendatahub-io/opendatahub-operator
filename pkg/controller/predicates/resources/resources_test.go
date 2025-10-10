package resources_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"

	. "github.com/onsi/gomega"
)

func TestAnnotationChanged(t *testing.T) {
	t.Parallel()

	const annotationName = "test-annotation"

	tests := []struct {
		name           string
		annotation     string
		oldAnnotations map[string]string
		newAnnotations map[string]string
		want           bool
	}{
		{
			name:           "annotation value changed",
			oldAnnotations: map[string]string{annotationName: "old-value"},
			newAnnotations: map[string]string{annotationName: "new-value"},
			want:           true,
		},
		{
			name:           "annotation value unchanged",
			oldAnnotations: map[string]string{annotationName: "same-value"},
			newAnnotations: map[string]string{annotationName: "same-value"},
			want:           false,
		},
		{
			name:           "annotation added",
			oldAnnotations: map[string]string{},
			newAnnotations: map[string]string{annotationName: "new-value"},
			want:           true,
		},
		{
			name:           "annotation removed",
			oldAnnotations: map[string]string{annotationName: "old-value"},
			newAnnotations: map[string]string{},
			want:           true,
		},
		{
			name:           "different annotation changed",
			oldAnnotations: map[string]string{annotationName: "value", "other-annotation": "old-value"},
			newAnnotations: map[string]string{annotationName: "value", "other-annotation": "new-value"},
			want:           false,
		},
		{
			name:           "nil annotations in both objects",
			oldAnnotations: nil,
			newAnnotations: nil,
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := NewWithT(t)

			got := resources.AnnotationChanged(annotationName).UpdateFunc(event.UpdateEvent{
				ObjectOld: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "test-pod",
						Annotations: tt.oldAnnotations,
					},
				},
				ObjectNew: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "test-pod",
						Annotations: tt.newAnnotations,
					},
				},
			})

			g.Expect(got).To(Equal(tt.want))
		})
	}
}
