package resources_test

import (
	"errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
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

func TestGetGroupVersionKindForObject(t *testing.T) {
	g := NewWithT(t)

	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
	g.Expect(appsv1.AddToScheme(scheme)).To(Succeed())

	t.Run("ObjectWithGVK", func(t *testing.T) {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gvk.Deployment)

		gotGVK, err := resources.GetGroupVersionKindForObject(scheme, obj)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(gotGVK).To(Equal(gvk.Deployment))
	})

	t.Run("ObjectWithoutGVK_SuccessfulLookup", func(t *testing.T) {
		obj := &appsv1.Deployment{}

		gotGVK, err := resources.GetGroupVersionKindForObject(scheme, obj)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(gotGVK).To(Equal(gvk.Deployment))
	})

	t.Run("ObjectWithoutGVK_ErrorInLookup", func(t *testing.T) {
		obj := &unstructured.Unstructured{}

		_, err := resources.GetGroupVersionKindForObject(scheme, obj)
		g.Expect(err).To(WithTransform(
			errors.Unwrap,
			MatchError(runtime.IsMissingKind, "IsMissingKind"),
		))
	})

	t.Run("NilObject", func(t *testing.T) {
		_, err := resources.GetGroupVersionKindForObject(scheme, nil)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("nil object"))
	})
}

func TestEnsureGroupVersionKind(t *testing.T) {
	g := NewWithT(t)

	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
	g.Expect(appsv1.AddToScheme(scheme)).To(Succeed())

	t.Run("ForObject", func(t *testing.T) {
		obj := &unstructured.Unstructured{}
		obj.SetAPIVersion(gvk.Deployment.GroupVersion().String())
		obj.SetKind(gvk.Deployment.Kind)

		err := resources.EnsureGroupVersionKind(scheme, obj)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(obj.GetObjectKind().GroupVersionKind()).To(Equal(gvk.Deployment))
	})

	t.Run("ErrorOnNilObject", func(t *testing.T) {
		err := resources.EnsureGroupVersionKind(scheme, nil)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("nil object"))
	})

	t.Run("ErrorOnInvalidObject", func(t *testing.T) {
		obj := &unstructured.Unstructured{}
		obj.SetKind("UnknownKind")

		err := resources.EnsureGroupVersionKind(scheme, obj)
		g.Expect(err).To(HaveOccurred())
		g.Expect(err.Error()).To(ContainSubstring("failed to get GVK"))
	})
}
