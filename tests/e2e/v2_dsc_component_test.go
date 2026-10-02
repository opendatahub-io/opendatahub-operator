package e2e_test

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"

	. "github.com/onsi/gomega"
)

type savedV2DSCField struct {
	path  []string
	value any
	found bool
}

// snapshotV2DSCFields restores only the named v2 spec fields. Component suites
// run concurrently and share one DSC, so cleanup must never replace its whole
// spec with an older snapshot of another component's changes.
func snapshotV2DSCFields(t *testing.T, tc *TestContext, paths ...[]string) {
	t.Helper()
	g := NewWithT(t)
	object := &unstructured.Unstructured{}
	object.SetGroupVersionKind(gvk.DataScienceClusterV2)
	g.Expect(tc.Client().Get(t.Context(), tc.DataScienceClusterNamespacedName, object)).To(Succeed())

	saved := make([]savedV2DSCField, 0, len(paths))
	for _, path := range paths {
		value, found, err := unstructured.NestedFieldCopy(object.Object, path...)
		g.Expect(err).NotTo(HaveOccurred())
		saved = append(saved, savedV2DSCField{path: path, value: value, found: found})
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		g.Eventually(func(g Gomega) {
			current := &unstructured.Unstructured{}
			current.SetGroupVersionKind(gvk.DataScienceClusterV2)
			g.Expect(tc.Client().Get(ctx, tc.DataScienceClusterNamespacedName, current)).To(Succeed())
			before := current.DeepCopy()
			g.Expect(restoreV2DSCFields(current, saved)).To(Succeed())
			g.Expect(tc.Client().Patch(ctx, current, client.MergeFrom(before))).To(Succeed())
		}).WithContext(ctx).WithPolling(2*time.Second).Should(Succeed(), "restore prior v2 DSC component fields")
	})
}

func restoreV2DSCFields(object *unstructured.Unstructured, saved []savedV2DSCField) error {
	for _, field := range saved {
		if field.found {
			if err := unstructured.SetNestedField(object.Object, field.value, field.path...); err != nil {
				return err
			}
		} else {
			unstructured.RemoveNestedField(object.Object, field.path...)
		}
	}
	return nil
}

func TestRestoreV2DSCFieldsTouchesOnlySelectedComponent(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	object := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{"components": map[string]any{
			"dashboard":   map[string]any{"managementState": "Managed"},
			"workbenches": map[string]any{"managementState": "Removed"},
		}},
	}}
	g.Expect(restoreV2DSCFields(object, []savedV2DSCField{
		{path: []string{"spec", "components", "dashboard"}, value: map[string]any{"managementState": "Removed"}, found: true},
		{path: []string{"spec", "components", "modelregistry"}},
	})).To(Succeed())
	state, found, err := unstructured.NestedString(object.Object, "spec", "components", "dashboard", "managementState")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(state).To(Equal("Removed"))
	state, found, err = unstructured.NestedString(object.Object, "spec", "components", "workbenches", "managementState")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue())
	g.Expect(state).To(Equal("Removed"))
	_, found, err = unstructured.NestedMap(object.Object, "spec", "components", "modelregistry")
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeFalse())
}
