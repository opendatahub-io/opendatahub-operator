//nolint:testpackage
package modules

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"

	. "github.com/onsi/gomega"
)

type testModuleHandler struct {
	BaseHandler
}

func (h *testModuleHandler) IsEnabled(_ *PlatformContext) bool {
	return true
}

func (h *testModuleHandler) BuildModuleCR(_ context.Context, _ client.Client, _ *PlatformContext) (*unstructured.Unstructured, error) {
	return nil, nil
}

func TestModuleStatusPredicates_StatusOnlyUpdatePassesThrough(t *testing.T) {
	g := NewWithT(t)

	testGVK := schema.GroupVersionKind{Group: "test.io", Version: "v1alpha1", Kind: "TestModule"}

	reg := DefaultRegistry()
	h := &testModuleHandler{
		BaseHandler: BaseHandler{
			Config: ModuleConfig{
				Name:   "test-module",
				CRName: "default-test",
				GVK:    testGVK,
			},
		},
	}

	reg.Add(h, WithRunlevel(dag.RL(20)))
	t.Cleanup(func() {
		reg.Disable("test-module")
	})

	preds := moduleStatusPredicates()
	g.Expect(preds).Should(HaveKey(testGVK))
	g.Expect(preds[testGVK]).Should(HaveLen(1))

	pred := preds[testGVK][0]

	oldObj := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "test.io/v1alpha1",
			"kind":       "TestModule",
			"metadata": map[string]any{
				"name":            "default-test",
				"resourceVersion": "100",
				"generation":      int64(1),
			},
			"status": map[string]any{
				"conditions": []any{
					map[string]any{
						"type":   "Ready",
						"status": "False",
					},
				},
			},
		},
	}

	newObj := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "test.io/v1alpha1",
			"kind":       "TestModule",
			"metadata": map[string]any{
				"name":            "default-test",
				"resourceVersion": "101",
				"generation":      int64(1),
			},
			"status": map[string]any{
				"conditions": []any{
					map[string]any{
						"type":   "Ready",
						"status": "True",
					},
				},
			},
		},
	}

	// Update (status) should pass through
	g.Expect(pred.Update(event.UpdateEvent{
		ObjectOld: oldObj,
		ObjectNew: newObj,
	})).Should(BeTrue(), "status-only update should trigger reconciliation")

	// Update (no change) should be filtered out
	g.Expect(pred.Update(event.UpdateEvent{
		ObjectOld: oldObj,
		ObjectNew: oldObj,
	})).Should(BeFalse(), "no-change update should not trigger reconciliation")

	// Delete should pass through
	g.Expect(pred.Delete(event.DeleteEvent{
		Object: oldObj,
	})).Should(BeTrue(), "delete should trigger reconciliation")

	// Create should be filtered out (owner controller creates these)
	g.Expect(pred.Create(event.CreateEvent{
		Object: newObj,
	})).Should(BeFalse(), "create should not trigger reconciliation")
}
