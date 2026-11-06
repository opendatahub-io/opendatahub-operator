package e2e_test

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/gomega"
)

type savedDSCField struct {
	path  []string
	value any
	found bool
}

// snapshotDSCFields restores only the named spec fields, read and written
// through the given DSC GVK. Component suites run concurrently and share one
// DSC, so cleanup must never replace its whole spec with an older snapshot of
// another component's changes. Use gvk.DataScienceClusterV2 for fields the v2
// view exposes; v3-only fields (e.g. kserve confidentialContainers) hide
// behind the conversion marker in the v2 view and need
// gvk.DataScienceCluster.
func snapshotDSCFields(t *testing.T, tc *TestContext, clusterGVK schema.GroupVersionKind, paths ...[]string) {
	t.Helper()
	g := NewWithT(t)
	object := &unstructured.Unstructured{}
	object.SetGroupVersionKind(clusterGVK)
	g.Expect(tc.Client().Get(t.Context(), tc.DataScienceClusterNamespacedName, object)).To(Succeed())

	saved := make([]savedDSCField, 0, len(paths))
	for _, path := range paths {
		value, found, err := unstructured.NestedFieldCopy(object.Object, path...)
		g.Expect(err).NotTo(HaveOccurred())
		saved = append(saved, savedDSCField{path: path, value: value, found: found})
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		g.Eventually(func(g Gomega) {
			current := &unstructured.Unstructured{}
			current.SetGroupVersionKind(clusterGVK)
			g.Expect(tc.Client().Get(ctx, tc.DataScienceClusterNamespacedName, current)).To(Succeed())
			before := current.DeepCopy()
			g.Expect(restoreDSCFields(current, saved)).To(Succeed())
			g.Expect(tc.Client().Patch(ctx, current, client.MergeFrom(before))).To(Succeed())
		}).WithContext(ctx).WithPolling(2*time.Second).Should(Succeed(), "restore prior DSC component fields")
	})
}

func restoreDSCFields(object *unstructured.Unstructured, saved []savedDSCField) error {
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
