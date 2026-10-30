package resources_test

import (
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	res "github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"

	. "github.com/onsi/gomega"
)

func TestCreatedOrUpdatedOrDeletedCRDGroupKindMatched(t *testing.T) {
	t.Parallel()

	g := NewWithT(t)
	expected := schema.GroupKind{Group: componentApi.GroupVersion.Group, Kind: componentApi.KserveKind}
	pred := resources.CreatedOrUpdatedOrDeletedCRDGroupKindMatched(expected)
	typed := &apiextensionsv1.CustomResourceDefinition{
		TypeMeta: metav1.TypeMeta{
			APIVersion: apiextensionsv1.SchemeGroupVersion.String(),
			Kind:       "CustomResourceDefinition",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "kserves." + expected.Group},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: expected.Group,
			Names: apiextensionsv1.CustomResourceDefinitionNames{Kind: expected.Kind, Plural: "kserves"},
		},
	}
	u, err := res.ToUnstructured(typed)
	g.Expect(err).NotTo(HaveOccurred())
	kueue := typed.DeepCopy()
	kueue.Name = "kueues." + expected.Group
	kueue.Spec.Names = apiextensionsv1.CustomResourceDefinitionNames{Kind: componentApi.KueueKind, Plural: "kueues"}
	unrelated, err := res.ToUnstructured(kueue)
	g.Expect(err).NotTo(HaveOccurred())

	crd := func(group, kind any) *unstructured.Unstructured {
		return &unstructured.Unstructured{Object: map[string]any{
			"spec": map[string]any{
				"group": group,
				"names": map[string]any{"kind": kind},
			},
		}}
	}

	tests := []struct {
		name string
		obj  client.Object
		want bool
	}{
		{name: "matching typed CRD", obj: typed, want: true},
		{name: "matching unstructured CRD", obj: u, want: true},
		{name: "unrelated Kueue kind in the same group", obj: unrelated},
		{name: "matching kind in a different group", obj: crd("other.example.io", expected.Kind)},
		{name: "missing spec", obj: &unstructured.Unstructured{Object: map[string]any{}}},
		{name: "missing group", obj: &unstructured.Unstructured{Object: map[string]any{
			"spec": map[string]any{"names": map[string]any{"kind": expected.Kind}},
		}}},
		{name: "missing kind", obj: &unstructured.Unstructured{Object: map[string]any{
			"spec": map[string]any{"group": expected.Group, "names": map[string]any{}},
		}}},
		{name: "invalid group type", obj: crd(int64(1), expected.Kind)},
		{name: "invalid kind type", obj: crd(expected.Group, int64(1))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g := NewWithT(t)
			g.Expect(pred.Create(event.CreateEvent{Object: tt.obj})).To(Equal(tt.want))
			g.Expect(pred.Update(event.UpdateEvent{ObjectOld: typed, ObjectNew: tt.obj})).To(Equal(tt.want))
			g.Expect(pred.Delete(event.DeleteEvent{Object: tt.obj})).To(Equal(tt.want))
		})
	}

	t.Run("OLM metadata updates still match a module CRD", func(t *testing.T) {
		t.Parallel()

		g := NewWithT(t)
		updated := u.DeepCopy()
		updated.SetAnnotations(map[string]string{"operatorframework.io/installed-alongside-test": "operator/new-version"})

		g.Expect(pred.Update(event.UpdateEvent{ObjectOld: u, ObjectNew: updated})).To(BeTrue())
	})
}
