//nolint:testpackage
package setup

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/upgrade"

	. "github.com/onsi/gomega"
)

func TestFilterDeleteConfigMapPredicateWithTypedAndUnstructured(t *testing.T) {
	const operatorNs = "test-operator-ns"

	r := &SetupControllerReconciler{}
	preds := r.filterDeleteConfigMap(operatorNs)

	tests := []struct {
		name string
		obj  client.Object
		want bool
	}{
		{
			name: "typed ConfigMap with correct namespace and label",
			obj: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "delete-cm",
					Namespace: operatorNs,
					Labels:    map[string]string{upgrade.DeleteConfigMapLabel: "true"},
				},
			},
			want: true,
		},
		{
			name: "typed ConfigMap wrong namespace",
			obj: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "delete-cm",
					Namespace: "other-ns",
					Labels:    map[string]string{upgrade.DeleteConfigMapLabel: "true"},
				},
			},
			want: false,
		},
		{
			name: "typed ConfigMap missing label",
			obj: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "delete-cm",
					Namespace: operatorNs,
				},
			},
			want: false,
		},
		{
			name: "unstructured ConfigMap with correct namespace and label",
			obj: func() client.Object {
				u := &unstructured.Unstructured{}
				u.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
				u.SetName("delete-cm")
				u.SetNamespace(operatorNs)
				u.SetLabels(map[string]string{upgrade.DeleteConfigMapLabel: "true"})
				return u
			}(),
			want: true,
		},
		{
			name: "unstructured ConfigMap wrong namespace",
			obj: func() client.Object {
				u := &unstructured.Unstructured{}
				u.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
				u.SetName("delete-cm")
				u.SetNamespace("other-ns")
				u.SetLabels(map[string]string{upgrade.DeleteConfigMapLabel: "true"})
				return u
			}(),
			want: false,
		},
		{
			name: "unstructured ConfigMap missing label",
			obj: func() client.Object {
				u := &unstructured.Unstructured{}
				u.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ConfigMap"))
				u.SetName("delete-cm")
				u.SetNamespace(operatorNs)
				return u
			}(),
			want: false,
		},
		{
			name: "unstructured non-ConfigMap with correct namespace and label",
			obj: func() client.Object {
				u := &unstructured.Unstructured{}
				u.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
				u.SetName("delete-cm")
				u.SetNamespace(operatorNs)
				u.SetLabels(map[string]string{upgrade.DeleteConfigMapLabel: "true"})
				return u
			}(),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			g.Expect(preds.Create(event.CreateEvent{Object: tt.obj})).
				To(Equal(tt.want), "CreateFunc")

			g.Expect(preds.Update(event.UpdateEvent{ObjectNew: tt.obj})).
				To(Equal(tt.want), "UpdateFunc")
		})
	}
}
